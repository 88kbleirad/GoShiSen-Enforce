# Nhật ký thay đổi (Changelog)

Trạng thái và lịch sử đáng chú ý của GoShiSen-Enforce. Chi tiết theo từng bản phát hành nằm trong `docs/release/` (vd: `v0.2.6-notes.md`, `proposal-next-version-scope.md`).

## Trạng thái phát hành (ảnh chụp, 2026-11-22)

- **v0.2.8** — bản phát hành mới nhất (đã công bố 2026-11-22): bản phát hành bảo mật/làm cứng — đọc file có giới hạn ở mọi chỗ đọc đầu vào của người vận hành, policy store riêng tư (0700/0600) với listener metrics có giới hạn thời gian, UCI `api_token` được ghi 0600, các tùy chọn yêu cầu xác thực khi đọc (D1), cân bằng lại govulncheck (D2). Ghi chú: `docs/release/v0.2.8-notes.md`; asset: [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.2.8).
- **v0.2.7** — bản phát hành trước (đã công bố 2026-11-21): bản phát hành làm cứng — ngữ nghĩa port-zero (khoảng trống tài liệu v0.2.6), bảo vệ vòng lặp `api_host`, làm cứng phương thức metrics, sửa tài liệu listener-defaults. Ghi chú phát hành và asset: [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.2.7).

## v0.2.8 (đã công bố 2026-11-22)

- **Siết quyền thư mục policy store + giới hạn listener metrics (#102):** policy store tạo thư mục với quyền `0700` và ghi file policy/`active.json` với quyền `0600` (trước là `0755`/`0644`), nên tài khoản cục bộ khác không đọc được policy đang áp dụng; listener HTTP metrics nay đặt `ReadTimeout`/`WriteTimeout` 10 giây và `IdleTimeout` 30 giây, khớp với listener API, nên client cục bộ bị treo không thể giữ kết nối (và goroutine) mãi. Test hồi quy: `TestOpenRestrictedPerms`.
- **Đọc file có giới hạn (#104):** đọc config/policy/store giờ từ chối file không-thường trước khi mở và giới hạn 16 MiB — đường dẫn policy trỏ vào /dev/urandom trước đây treo vô hạn (bộ nhớ ~1 GiB/s).
- **CI mạnh hơn (#105–#107):** sweep bất thường CLI 257 case và các mục tiêu fuzz config đã được đưa vào repo và chạy trong CI, cùng staticcheck (baseline rỗng) và govulncheck source mode.
- **UCI api_token (#108, đóng #103):** cấu hình OpenWrt giờ đặt được token API; config daemon được render có chmod 600 và charset token bị giới hạn (A-Za-z0-9._-); init script cảnh báo khi listener API bật mà không có token.
- **Đọc thống kê queue cũng có giới hạn (mở rộng P1.1 của v0.2.8):** `packet.ReadQueueStats` (đọc `/proc/net/netfilter/nfnetlink_queue`, dùng bởi watcher cảnh báo overflow) là chỗ `os.ReadFile` không giới hạn cuối cùng trong cây mã; nay đi qua `internal/safefile.ReadBounded`, nên file không-thường (FIFO, device, thư mục) hoặc dữ liệu vượt hạn sẽ fail-closed thay vì treo daemon. Test hồi quy bao phủ: file thường, file thiếu, FIFO (không được block), thư mục và kích thước vượt hạn.
- **Kiểm tra đường dẫn policy lúc khởi động cũng có giới hạn (F-4a/F-4b):** bước tiền-kiểm tra của daemon mở `policy_file` bằng `os.Open` thường, nên một đường dẫn tới FIFO sẽ chặn khởi động vô hạn, còn thư mục/thiết bị vẫn được chấp nhận và chỉ lỗi sau đó. Nay bước kiểm tra đi qua bộ đọc có giới hạn (mở non-blocking, kiểm tra file thường, giới hạn 16 MiB), nên đường dẫn không dùng được sẽ fail nhanh với lỗi rõ ràng `policy_file:`. Test bao phủ: FIFO (không được block), thư mục, thiết bị, vượt hạn và file thường.

## Chi tiết đã chuyển ra khỏi README

- Bộ tích hợp: 21 kịch bản netns (e2e, state, NAT, crash, overflow trong `tests/integration/`), gated bằng root, chạy với `sudo make integration`; CI chạy toàn bộ bộ trên runner GitHub (21/21 PASS, không có skip bắt buộc).
- Xác thực OpenWrt: OpenWrt 25.12.6 x86/64 (kernel 6.12.112); dịch vụ tắt mặc định; IPv6 stateful ACCEPT đã xác minh trên kernel 6.12.112 với mẫu quy tắc `ESTABLISHED/RELATED` hai chiều chuẩn — `docs/openwrt.md`, `docs/release/issue-5-investigation.md`.
- Đường nóng không cấp phát (0 allocs/gói); benchmark: `docs/design/performance.md`.
- Danh mục metric (12 họ) và thiết lập Grafana: `docs/release/metrics-grafana-validation.md`.
- Cấu trúc kho: `docs/developer-guide.md`.

## Các bản sửa kiểm toán (phát hành trong v0.2.6, 2026-11-18)

- **Phân tích cổng nghiêm ngặt (F-001):** các bộ so khớp `src_port`/`dst_port` trong chính sách và giá trị env `GOSHISEN_API_PORT`/`GOSHISEN_METRICS_PORT` giờ phải là số nguyên thuần. Trước đây `fmt.Sscanf("%d")` âm thầm chấp nhận ký tự rác theo sau (`"80evil"` → 80), nên một lỗi gõ có thể thay đổi chính sách được thi hành hoặc cổng ràng buộc thay vì làm hỏng cấu hình.
- **Gán lỗi backend (F-002):** lỗi socket NFQUEUE phía nhận giờ được báo cáo là `kind=receive` (khớp với bộ nhãn `goshisen_backend_errors_total` đã ghi lại) thay vì `kind=send`.

## Các bản sửa bảo mật (phát hành trong v0.2.6, 2026-11-19)

- **Bảo vệ chèn tên thẻ phát hành (S-003):** workflow phát hành giờ xác thực tên thẻ được đẩy lên theo `^v[0-9]+\.[0-9]+\.[0-9]+$` trong bước đầu tiên trước mọi thứ khác, truyền qua biến môi trường và đặt trong dấu nháy tại mọi nơi sử dụng; chế độ production của `apk-sign.yml` xác thực ref của nó cùng cách. Job CI `release-tag-guard` chứng minh các fixture tên thẻ chứa ký tự đặc biệt shell bị từ chối trước khi dùng. Không kèm bước tái tạo công khai cho đến khi bản này hạ cánh.
- **Ảnh container ghim theo digest (S-004):** `openwrt/sdk` (`apk-sign.yml`), `golang:1.25` và `gcr.io/distroless/base-debian13` (Dockerfile) được ghim theo digest; ghi chú cập nhật tại mỗi lần ghim.
- **Hành động CI trên runtime Node 24:** actions/checkout v7.0.1, actions/setup-go v7.1.0, actions/upload-artifact v7.0.2 (ghim SHA), loại bỏ cảnh báo hết hạn Node.js 20 trên mọi job.
- **Dockerfile và CI trên go1.27.4 (S-001):** ảnh nền Dockerfile được nâng từ golang:1.25 (go1.25.14, 11 lỗ hổng stdlib có thể tiếp cận) lên `golang:1.27.4@sha256:205c7636...`; mọi job CI cài go1.27.4.
- **Cổng payload apk (S-005):** payload apk v0.2.5 đã công bố được build với go1.27.1 → 11 lỗ hổng stdlib có thể tiếp cận (issue #78). Đã đóng trong v0.2.6 (payload apk + cả hai kho lưu trữ CLI build với go1.27.4). Hai cổng chặn fail-closed trên đường ký giờ chạy trong chế độ production của `apk-sign.yml` trước khi ký, cùng các job hồi quy CI:
  - `scripts/check-apk-payload-go.sh` từ chối mọi apk có payload được build với Go < `MIN_GO_VERSION` (`packaging/go-toolchain.env`, 1.27.4);
  - `scripts/check-apk-payload-govuln.sh` chạy govulncheck chế độ binary trên payload đã giải nén và thất bại nếu có lỗ hổng stdlib/module có thể tiếp cận không nằm trong `packaging/govulncheck-allowlist.txt` (trống theo mặc định). Payload phải mang buildinfo Go đọc được, nên công thức gói v0.2.6 bỏ `-buildid=` và đặt `STRIP:=true` — `sstrip` mặc định của SDK gỡ bảng chuỗi mục ELF, làm hỏng `go version -m` và quét chế độ binary ngay cả khi giữ `-buildid=` (công thức v0.2.5 bỏ cả hai, làm hỏng hoàn toàn việc quét chế độ binary).
  Feed được nướng sẵn của ảnh SDK hiện tại CHƯA build Go 1.27.4 (mặc định feed go1.26.9, golang1.27 go1.27.1; ghim lại feed về commit mới hơn làm hỏng quét metadata của SDK này) — bản build v0.2.6 gắn toolchain go1.27.4 được xác minh sha256 (`scripts/sdk-build-v026.sh`, phương án 3 trong báo cáo S-005). Apk v0.2.5 đã phân phối không thay đổi.
- **Toolchain ghim go1.27.4 (S-001):** 11 lỗ hổng thư viện chuẩn có thể tiếp cận (net/http Range-header DoS, chạy đua MaxBytesReader, chạy đua HPACK http2, crypto/x509) dưới go1.27.1 đã đóng; `golang.org/x/net` v0.62.0 và `golang.org/x/sys` v0.49.0. Chỉ thị `go` giờ là `go 1.26.0` (yêu cầu Go ≥ 1.26; CI cài trực tiếp go1.27.4).
- **queue_num giới hạn 0..65534 (S-002):** queue_num 65535 từng được chấp nhận nhưng không bao giờ cài được (quy tắc queue IPv6 tại queue_num+1 sẽ thành `queue to 65536`, ngoài phạm vi u16); cấu hình giờ từ chối nó kèm thông báo rõ ràng.

## Hiệu chỉnh (tài liệu hậu v0.2.6, 2026-11-19; tái xác minh 2026-11-20)

- **Mặc định listener HTTP (hiệu chỉnh tài liệu):** ghi chú v0.2.6,
  cảnh báo bảo mật v0.2.5 và tài liệu OpenWrt từng nói các listener
  HTTP "bị tắt theo mặc định". Thực tế mặc định tích hợp của daemon là
  API `127.0.0.1:8080` và metrics `127.0.0.1:9090` (loopback) sau khi
  dịch vụ được bật; cấu hình OpenWrt được phân phối lưu
  `api_port '0'`/`metrics_port '0'`, nhưng v0.2.6 bỏ qua giá trị `0`
  từ tệp/cờ (quay về mặc định; `internal/config/config.go` và
  `internal/cli/cli.go`). Cách duy nhất để tắt listener hiện nay là
  `GOSHISEN_API_PORT=0` / `GOSHISEN_METRICS_PORT=0`, hoặc giữ dịch vụ
  tắt (`enabled=0`). Không có thay đổi hành vi mã; mức độ nghiêm trọng
  vẫn là Low (mặc định) / Medium (`api_host` không-loopback).
  Bản vá mã để tôn trọng `0` tường minh dự kiến ở v0.2.7.
- **Mặc định listener đo được (2026-11-20), có cả kho lưu trữ CLI:**
  cùng hiệu chỉnh áp dụng cho **kho lưu trữ CLI** v0.2.5/v0.2.6,
  trước đây chỉ được mô tả là "chỉ loopback". Đo được khi không có
  cấu hình/env/cờ, chúng lắng nghe trên `127.0.0.1:8080` +
  `127.0.0.1:9090`; YAML `api_port: 0`, giá trị YAML trống và
  `--api-port 0` bị bỏ qua ở đó, trong khi `GOSHISEN_API_PORT=0` tắt.
  OpenWrt: cài đặt mới gói apk 0.2.5-r1 / 0.2.6-r1 đã ký với cấu
  hình đi kèm, và nâng cấp tại chỗ 0.2.3-r1 → 0.2.6-r1 trong khi đang
  chạy, cả hai đều kết thúc với hai listener loopback hoạt động; cấu
  hình không có các tùy chọn (hoặc để trống) hủy bỏ trong script init
  (`api_port '' out of range`) mà không có listener. Thứ tự ưu tiên
  được xác nhận bằng `goshisen config show`: mặc định < tệp < env <
  cờ. Bảng bằng chứng: `docs/security/advisory-v0.2.5-go-toolchain.md`
  § "Measured listener defaults"; kiểm thử hợp đồng:
  `tests/package/listener_defaults_contract_test.go`. Bản vá mã tôn
  trọng `0` tường minh có trên `main` sau thẻ v0.2.6 nhưng **chưa có
  trong bất kỳ artifact nào đã công bố** (ứng viên v0.2.7).

## Chưa phát hành (sau v0.2.6)

- **Giá trị `0` tường minh tắt listener HTTP (khắc phục lỗ hổng tài
  liệu v0.2.6):** `api_port: 0` / `metrics_port: 0` trong tệp cấu hình
  và `--api-port 0` / `--metrics-port 0` trên dòng lệnh giờ tắt
  listener API / metrics thay vì bị coi là "chưa đặt" và quay về
  8080/9090. Lớp tệp theo dõi sự hiện diện của khóa
  (`File.UnmarshalYAML`); lớp cờ dùng `flag.Value` ghi nhận sự hiện
  diện (`portFlag`, cũng từ chối giá trị ngoài phạm vi tại thời điểm
  phân tích). Cổng chưa đặt vẫn giải quyết về mặc định loopback tích
  hợp. Cấu hình OpenWrt được phân phối (`option api_port '0'`) giờ tắt
  cả hai listener như mong muốn; `GOSHISEN_API_PORT=0` /
  `GOSHISEN_METRICS_PORT=0` hoạt động như trước. Kiểm thử:
  `TestLoadFileZeroPortsDisable`, `TestLoadFilePortPresence`,
  `TestLoadFlagZeroPortDisables`, `TestLoadFilePortRejectsNonScalar`,
  `TestParsePortZeroFlags`, `TestParsePortInvalidValues`. Không thay
  đổi mức độ nghiêm trọng bảo mật (mặc định loopback-only).
- **Fixture lab bật listener metrics tường minh:** ảnh lab QEMU
  (`scripts/openwrt-image-prep.sh`) là người dùng duy nhất cần listener —
  nó đọc bộ đếm phán quyết từ `127.0.0.1:9090/metrics` — nên giờ nó
  ghi `metrics_port: $LAB_METRICS_PORT` (9090) thay vì dựa vào `0`
  quay về mặc định; `api_port` vẫn là `0` (chính sách được áp dụng qua
  SIGHUP). Cả hai script e2e đọc `${LAB_METRICS_PORT:-9090}` và thất
  bại nhanh với `ENV: metrics listener unreachable` khi snapshot không
  lấy được metrics, thay vì thông báo gây hiểu lầm "no daemon DROP
  verdicts recorded". Kiểm thử: `TestProvisionLabMetricsPort`.
