# Hiệu năng — Đường cơ sở, Profiling, Tối ưu hóa

**Trạng thái: hoàn tất.** Nhật ký benchmark thô trong `evidence/perf-benchmark/`
(`base_idle_*_run{1,2,3}.txt` = trước tối ưu hóa, chạy trong môi trường
rảnh rỗi; `opt_*_run{1,2,3}.txt` = sau tối ưu hóa, cùng máy).
Báo cáo này chỉ khẳng định các cải thiện được xác minh bằng ba lần chạy cho mỗi
phía với trung vị + độ lệch chuẩn tổng thể (σ) và siêu dữ liệu môi trường.

## 0. Môi trường (giống hệt cho các lần chạy baseline và tối ưu hóa)

| Trục | Giá trị |
| --- | --- |
| Máy chủ | máy dev (uid 1000, không root, không CAP_NET_ADMIN) |
| CPU | Intel Core i5-11400H thế hệ 11, 12 luồng, 2,70 GHz base / 4,5 GHz boost |
| RAM | 32 GB (31,8 GiB tổng) |
| OS / kernel | Linux 7.3.3-1-cachyos |
| Go | go1.27.3-X:nodwarf5 linux/amd64 |
| Commit (trước) | `bfbf813` (worktree baseline, sạch) |
| Commit (sau) | working tree tại `bfbf813` + các thay đổi của giai đoạn này (chưa commit) |
| Chế độ bench | `go test -bench=. -benchmem -count=1 -run '^$'`, 3 lần chạy mỗi phía, cùng ngày/rảnh rỗi |
| Phương pháp | trung vị + pstdev qua 3 lần chạy; thay đổi khi |Δ| > 3×σ |

Các model: `internal/policy` (engine), `internal/packet` (parser + pipeline
quyết định backend), `internal/metrics` (quan sát kết quả). Thông lượng
socket NFQUEUE thực tế bị giới hạn bởi kernel/root; benchmark pipeline đo
toàn bộ đường đi mỗi-gói trong tiến trình *bao gồm* cả quan sát metric,
không tính các syscall socket netlink.

## 1. Đường cơ sở (trước tối ưu hóa)

Profile đường-nóng (pprof, `BenchmarkKingProcessNewAccept`): `fmt.doPrintf`
≈ 55% CPU — khóa tập đã-chấp-nhận (`tupleKey`) được xây bằng
`fmt.Sprintf` cho mỗi gói có trạng thái; `runtime.mallocgc` ≈ 19% (allocs).
Cấp phát mỗi-gói trên pipeline đầy đủ: **15–16 allocs, ~312 B**.

| Benchmark | Trung vị cơ sở ns | σ cơ sở | Allocs/op cơ sở |
| --- | ---: | ---: | ---: |
| Parse IPv4 TCP | 16.7 | 0.0 | 0 |
| Parse IPv4 UDP | 16.8 | 0.1 | 0 |
| Parse IPv6 TCP | 18.1 | 0.0 | 0 |
| Decide small policy (NEW accept) | 38.7 | 0.0 | 0 |
| Decide small policy (ESTABLISHED) | 26.9 | 0.4 | 0 |
| Decide 200-rule policy (worst-case) | 2181 | 38.3 | 0 |
| King.Process NEW accept | 934 | 16.7 | 16 |
| King.Process ESTABLISHED reply | 409 | 4.6 | 7 |
| tupleKey | 359 | 4.0 | 6 |
| Pipeline NEW accept (e2e) | 1129 | 24.7 | 15 |
| Pipeline ESTABLISHED reply | 634 | 2.8 | 7 |
| Pipeline NEW default-drop | 668 | 8.2 | 8 |
| Pipeline metrics-only | 86.5 | 0.6 | 1 |
| Độ trễ p50 / p95 / p99 (µs) | 1.039 / 1.719 / 2.686 | 7.4 | 15 |

## 2. Nút thắt cổ chai

Bản thân đường quyết định (parse + khớp prefix/cổng/trạng thái) vốn đã
zero-alloc. Chi phí nóng là **tập đã-chấp-nhận có trạng thái**:

- `tupleKey` dùng `fmt.Sprintf("%s|%d|%s|%d|%d", …)` → ~360 ns, 6 allocs
  mỗi gói (được `IsAccepted` gọi cho mọi gói có trạng thái và được
  `MarkAccepted` gọi trên mỗi NEW-accept).
- `sync.Map` với khóa chuỗi làm tăng cấp phát map-node.
- Metrics: hai lần chiếm mutex riêng biệt mỗi gói (`ObserveLatency`
  + `ObserveVerdict`), tăng map khóa chuỗi → ~86 ns, 1 alloc.
- `Rule.matches` tính lại `Prefix.Contains` qua
  `netip.PrefixFrom(...).Masked()` cho mỗi quy tắc mỗi gói.

## 3. Thay đổi (có kiểm soát, bảo toàn ngữ nghĩa)

1. `internal/policy/engine.go` — `tupleKey` chuỗi → struct `tupleKey
   {lo, hi uint64; ppm uint64}` (5-tuple đóng gói: v4 như hai nửa 32-bit;
   v6 như cặp 128-bit đầy đủ; protocol<<32 | srcPort<<16 |
   dstPort trong 64 bit). Chuẩn hóa reply được
   bảo toàn (hoán đổi src/dst + cổng khi
   `Direction == DirReply`). `sync.Map` → `sync.RWMutex` + map.
   Được neo bởi `internal/policy/tuplekey_test.go` (ánh xạ reply v4/v6,
   không-va-chạm cho kết nối phân biệt).
2. `internal/policy/engine.go` — `Rule` được nén gọn: `States []State` →
   `StateMask uint16` (kiểm tra một bit, không quét slice); `*PortRange` →
   giá trị `PortRange` + các bool `HasSrcPort`/`HasDstPort`.
3. `internal/metrics/metrics.go` — `ObserveVerdict`+`ObserveLatency`
   → một `ObservePacket` duy nhất dưới **một** lock; các map khóa chuỗi →
   mảng cố định `[2][3]int64` / `[5]int64` được lập chỉ mục theo enum int.
   Các loại enum (`BackendErrorKind`, `OverflowCause`,
   `PolicyUpdateOutcome`) trở thành int có kiểu với kết xuất `String()`;
   đầu ra văn bản Prometheus giống hệt byte. Bộ quyết định `internal/app`
   được cập nhật sang API mới.
4. `internal/packet/capture.go` — phân tích thuộc tính trong `evaluate`
   chuyển từ closure-walk (`parseAttrs` + closure mỗi gói) sang
   một switch inline (`parseNFQueueAttrs`); bộ đệm thông điệp verdict giờ
   được pool hóa (`verdictBufPool`), loại bỏ một `make` mỗi verdict.

Mã parser không đổi; chênh lệch nhỏ trước/sau của nó là nhiễu môi trường.

## 4. Kết quả (3 lần chạy mỗi máy, cùng máy rảnh rỗi, trung vị + σ)

| Benchmark | Trung vị cơ sở | Trung vị tối ưu | Δ | Allocs cơ sở | Allocs tối ưu | σ tối ưu |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| King.Process NEW accept | 934 ns | 103 ns | **−89%** | 16 | 0 | 3.5 |
| King.Process ESTABLISHED | 409 ns | 53 ns | **−87%** | 7 | 0 | 1.8 |
| tupleKey | 359 ns | 4.4 ns | **−99%** | 6 | 0 | 0.1 |
| Pipeline NEW accept (e2e) | 1129 ns | 253 ns | **−78%** | 15 | 0 | 5.7 |
| Pipeline ESTABLISHED reply | 634 ns | 193 ns | **−70%** | 7 | 0 | 3.5 |
| Pipeline NEW default-drop | 668 ns | 206 ns | **−69%** | 8 | 0 | 3.1 |
| Pipeline metrics-only | 86.5 ns | 15.8 ns | **−82%** | 1 | 0 | 0.0 |
| Decide small NEW | 38.7 ns | 35.5 ns | −8% | 0 | 0 | 0.3 |
| Decide small ESTABLISHED | 26.9 ns | 23.6 ns | −12% | 0 | 0 | 0.5 |
| Decide 200 rules | 2181 ns | 2035 ns | −7% | 0 | 0 | 14.5 |
| Parse IPv4 TCP | 16.7 ns | 17.9 ns | +7% | 0 | 0 | 0.2 |
| Độ trễ p50 (µs) | 1.039 | 0.306 | **−71%** | 15 | 0 | 3.3 |
| Độ trễ p95 (µs) | 1.719 | 0.31 | **−82%** | | | | |
| Độ trễ p99 (µs) | 2.686 | 0.48 | **−82%** | | | | |

### Ghi chú

- **Zero allocations** trên pipeline quyết định đầy đủ sau khi tối ưu hóa
  (parser, engine, metrics, dựng thông điệp verdict).
- Công suất trong-tiến-trình mỗi-gói ở 253 ns/op ≈ **~3,9 triệu gói/giây**
  một-lõi (chỉ quyết định; thông lượng NFQUEUE thực tế thêm syscall netlink
  và sao chép kernel, đồng thời bị giới hạn bởi root — được đo trong lab).
- Không khẳng định cải thiện parse (mã không đổi; nằm trong nhiễu σ).

## 5. Xác minh ngữ nghĩa (hành vi không đổi)

| Kiểm tra | Bằng chứng |
| --- | --- |
| Số worker | 2 goroutine (một cho mỗi socket queue, IPv4 + IPv6), `Backend.Run`; không tồn tại worker pool cấu hình được — pipeline quyết định là đơn-luồng mỗi socket |
| Kích thước chính sách | `BenchmarkDecideLargePolicy` (200 quy tắc) được xác minh; tỉ lệ là O(quy tắc) khớp-đầu-tiên |
| IPv4/IPv6 | `BenchmarkParseIPv4TCP/UDP`, `BenchmarkParseIPv6TCP`, `TestParseIPv6*`, `TestPublishPolicyIPv6`, `TestTupleKeySemantics/ipv6` |
| Ngữ nghĩa có trạng thái | `TestStatefulDecide`, `TestStatefulKingProcess`, `TestPolicyReloadKeepsEstablished`, `TestTupleKeySemantics` (chuẩn hóa reply), bench established-reply pipeline |
| Tải lại nguyên tử | `TestKingAtomicReplace`, `TestKingConcurrentReplace`, `TestPublishPartialFailureKeepsOld`, `TestPublishConcurrentSerializes`, `TestRunSIGHUPReload`, `TestReloadFromFile*` |
| Default-drop | `TestDefaultDeny`, `TestDecideNoMatchDefaultDrop`, drop chính sách chuỗi kernel (kiến trúc §2.1) |
| Fail-closed | `TestFailClosedNeverRequestsFailOpen`, verdict luôn được gửi (`TestEvaluateVerdicts`, `TestEvaluatePanicSafety`) |

Tất cả `go test -race ./...` đều qua; `make check` (fmt + vet + test +
build) qua.

## 6. Tệp

- Bộ benchmark: `tests/benchmark/` (rỗng), `internal/packet/parser_bench_test.go`, `internal/packet/pipeline_bench_test.go`, `internal/policy/engine_bench_test.go`.
- Test mới: `internal/policy/tuplekey_test.go`.
- Bằng chứng: `evidence/perf-benchmark/` (env.md, base_idle_*, opt_*, base_*).
