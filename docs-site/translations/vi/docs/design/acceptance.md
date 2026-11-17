# Báo cáo chấp nhận cuối — GoShiSen-Enforce

Nhánh: `phase7-baseline`. Commit: `edb38b6` (0+1), `4097e05` (2), `7b9f478` (4), `ae9eee6` (5). Môi trường: không đặc quyền (uid 1000, không `nft`, không CAP_NET_ADMIN) — mọi lần chạy tích hợp đặc quyền vẫn bị gating bởi môi trường và **không** được tuyên bố là đã pass.

## Bằng chứng xác minh

| Gate | Kết quả |
| --- | --- |
| `gofmt -l .` | sạch |
| `go mod verify` | pass (gồm cold clone) |
| `go vet ./...` | pass |
| `go test -race ./...` | pass — mọi package (test tích hợp skip với lý do tường minh không-root) |
| `go build ./...` | pass (gồm cold clone) |
| `make check` các giai đoạn | tất cả pass (fmt/vet/test/build) |
| Smoke CLI (version, config check, policy validate, policy simulate) | đã xác minh với nhị phân thật; `--state new` được tài liệu hoá |
| OpenAPI vs routes | khớp (không thay đổi API trong hardening) |
| Cold clone (git archive → fresh checkout) | `go mod verify`, gofmt, vet, race tests, build đều pass |

## Trạng thái phase

| Phase | Trạng thái |
| --- | --- |
| 0 Baseline & toolchain | **done** — báo cáo trong `docs/design/baseline.md`; CI Go 1.22→1.25 (2 jobs); sàn docs → 1.25; không có tệp chưa commit ở baseline |
| 1 Queue overflow | **done (code+tests)** — parser thống kê procfs kernel, `goshisen_queue_overflow_total{cause=kernel_drop|user_drop}` giới hạn, watcher 1 s với WARN giới hạn tần suất, `queue_maxlen` config/env/flag, unit test bất biến fail-open, `overflow_test.go` gated bằng root |
| 2 Crash/restart | **done (code+tests)** — sẵn sàng gated trên hook+queue (bị xoá lúc tắt; đã unit test), tích hợp SIGKILL fail-closed + phục hồi khởi động lại, unit systemd + vòng supervisor lab |
| 3 Bộ đặc quyền | **environment-gated** — điều kiện tiên quyết được kiểm tra (root/nft/netns/Go); script lab từ chối sạch khi không-root; mọi test tích hợp skip với lý do tường minh; CI có job đặc quyền |
| 4 Rà soát bảo mật | **done** — 9/9 PASS; 2 ghi chú mức thấp đã sửa (enum lỗi backend có kiểu; gỡ token khỏi cấu hình mẫu). Bao phủ hồi quy hiện hữu |
| 5 Docs & release | **done (code/docs)** — architecture được dung hoà (gỡ các tuyên bố conntrack mirror), hướng dẫn người dùng/nhà phát triển, README, SECURITY.md, OpenAPI đã xác minh, checklist phát hành đã tạo, build cold-clone pass |

## Còn lại trước phát hành (yêu cầu bởi checklist)

- Chạy `sudo scripts/lab.sh up && sudo make integration && sudo scripts/lab.sh down` trên máy chủ đặc quyền; xác minh cả 21 test tích hợp (S1–S12 + NAT + overflow + crash) pass và teardown sạch.
- Chỉ gắn thẻ `v0.1.0` sau khi bộ đặc quyền pass (theo `docs/release-checklist.md`).

## Quyết định thiết kế đáng chú ý

- Thống kê tràn đến từ `/proc/net/netfilter/nfnetlink_queue` (phạm vi namespace; API kernel duy nhất lộ `queue_dropped`/`queue_user_dropped`) — không phải thuộc tính netlink (không tồn tại thuộc tính nào).
- Fail-closed mang tính cấu trúc: backend không bao giờ đặt `NFQA_CFG_F_FAIL_OPEN`; bất biến được cố định bằng test.
- Sẵn sàng được chuyển sau khi bind queue; hook cài trước, nên tường lửa fail closed suốt quá trình khởi động.
- Không mock tuyên bố hành vi kernel: test parser dùng byte netlink tổng hợp; test watcher dùng dòng proc tổng hợp; test tích hợp dùng lưu lượng thật trong namespace thật.
