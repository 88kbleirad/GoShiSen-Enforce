# Báo cáo Baseline — GoShiSen-Enforce (nhánh `phase7-baseline`)

Ngày: 2026-11-09. Trạng thái kho lưu trữ tại commit `24bac6b` (nhánh `phase7-baseline`, cây làm việc sạch — không có thay đổi chưa commit, không có tệp staged, không có tệp untracked).

## 1. Các tính năng hiện đã triển khai

| Khu vực | Gói | Trạng thái |
| --- | --- | --- |
| Công cụ chính sách (YAML, nghiêm ngặt, khớp đầu tiên, snapshot nguyên tử) | `internal/policy` | hoàn chỉnh, có unit test |
| Backend NFQUEUE (nfnetlink_queue trực tiếp qua mdlayher/netlink) | `internal/packet` | hoàn chỉnh, có unit test, đã fuzz |
| Bộ phân tích IPv4/TCP/UDP (không panic; chính sách fragment/protocol) | `internal/packet` | hoàn chỉnh, fuzz sạch |
| Hook nftables (bảng `ip goshisen` do ứng dụng sở hữu, cài đặt nguyên tử, dọn dẹp) | `internal/hook` | hoàn chỉnh |
| Kho lưu trữ chính sách bền vững (tệp phiên bản, con trỏ hoạt động nguyên tử) | `internal/store` | hoàn chỉnh |
| REST API (bind localhost, xác thực bearer, CAS phiên bản, audit, health/readiness) | `internal/api` | hoàn chỉnh |
| Metrics (định dạng text Prometheus với cardinality có giới hạn) | `internal/metrics` | hoàn chỉnh |
| Vòng đời daemon (policy-before-hook, tín hiệu nhẹ nhàng, SIGHUP reload) | `internal/app` | hoàn chỉnh |
| NAT (DNAT + MASQUERADE, phía kernel, giao dịch nguyên tử) | `internal/hook` | hoàn chỉnh |
| Đóng gói Docker (multi-stage, distroless, không privileged) | `deployments/docker/Dockerfile` | hoàn chỉnh |
| Khung lab (netns + veth cô lập, teardown chắc chắn) | `scripts/lab.sh` | hoàn chỉnh |
| Bộ kiểm thử tích hợp (lưu lượng thực, root-gated, 17 kịch bản) | `tests/integration` | có mặt; **không thể chạy khi không phải root** |

Cổng baseline (chạy trên Go 1.27.1): `go mod verify` ✓, gofmt sạch ✓, `go vet ./...` ✓, `go test -race ./...` ✓ (mọi gói đều pass; kiểm thử tích hợp bỏ qua kèm lý do rõ ràng), `go build ./...` ✓.

## 2. Các tiêu chí chấp nhận còn thiếu hoặc chưa hoàn chỉnh

- Các kịch bản tích hợp đặc quyền Enforcement/stateful/NAT: chưa bao giờ được thực thi trong môi trường này (không phải root). Mã đã tồn tại; `checkPrereqs` chặn dựa trên euid 0 + các công cụ + tạo netns.
- Hardening/phát hành: mọi mục đều còn mở tại baseline — quan sát tràn hàng đợi, hardening crash/restart, cấu hình supervisor, đóng gói phát hành.
- Tài liệu kiến trúc (`docs/design/architecture.md`) vẫn mô tả thiết kế userspace conntrack-mirror mà phần triển khai không có (triển khai đọc trực tiếp metadata CT theo từng gói từ NFQUEUE). Cần hòa giải sự lệch tài liệu/triển khai.
- Tiêu đề hướng dẫn sử dụng vẫn ghi "foundation" tại baseline; README ghi trạng thái kỷ nguyên NAT. Cần đồng bộ tài liệu.

## 3. Các mâu thuẫn toolchain

| Ở đâu | Trước (baseline) | Yêu cầu |
| --- | --- | --- |
| `go.mod` | `go 1.25.0` | — |
| Dependencies (netlink v1.11.2, socket v0.6.0, x/sys, x/net, x/sync) | tất cả khai báo mức sàn `go 1.25.0` | Go ≥ 1.25 |
| CI `.github/workflows/ci.yml` (các job +build và integration) | `go-version: "1.22"` | **phải ≥ 1.25** — job không thể biên dịch module |
| `deployments/docker/Dockerfile` | `golang:1.25` | ✓ nhất quán |
| `docs/design/architecture.md` §8.1 | "Go ≥ 1.22" | cũ → Go ≥ 1.25 |
| `docs/design/mvp-scope.md` §7 | "Go ≥ 1.22" | cũ → Go ≥ 1.25 |
| `docs/developer-guide.md` | "Go ≥ 1.22 (developed on Go 1.27)" | cũ → Go ≥ 1.25 |

Giải pháp đã áp dụng: CI `go-version` → `"1.25"` (cả hai job); mức sàn trong tài liệu → `Go ≥ 1.25`. `go.mod` giữ nguyên `1.25.0`; Dockerfile không đổi. Toolchain cục bộ đã cài là Go 1.27.1 (≥ sàn, OK).

## 4. Các kiểm thử cần root / CAP_NET_ADMIN / nftables / netns

Tất cả nằm trong `tests/integration/` (bỏ qua kèm lý do rõ ràng khi không phải root qua `checkPrereqs`):

- `e2e_test.go`: TestLabAllowedTCP, TestLabAllowedUDP, TestLabDeniedTraffic, TestLabDefaultDrop, TestLabQueueNoListener, TestLabShutdownClean, TestHookCleanupScope.
- `state_test.go`: TestStateTCPHandshakeAndReply, TestStateUnsolicitedInbound, TestStateInvalidConnection, TestStateConnectionTimeout, TestStateUDPTimeout, TestStateReloadWhileEstablished.
- `nat_test.go`: TestNATDnat, TestNATMasquerade, TestNATReturnTraffic, TestNATWithFiltering, TestNATReloadWhileConnections.
- `overflow_test.go` (mới, hardening): TestQueueOverflowKernelDrop.

## 5. Các tệp chưa commit / các xóa rủi ro

- Tại baseline: **không có** — cây làm việc sạch trên `main`; không có tệp chưa commit, không có xóa rủi ro.
- Tài nguyên lab được chứa trong namespace (`gsh_*`); `scripts/lab.sh down` chỉ xóa các lab namespace/veth và không bao giờ chạm vào tường lửa máy chủ.

## 6. Các thay đổi an toàn nhỏ nhất đề xuất

1. CI: `go-version: "1.22"` → `"1.25"` trong cả hai job (đã làm).
2. Tài liệu: mức sàn Go 1.22 → 1.25 trong architecture.md, mvp-scope.md, developer-guide.md (đã làm).
3. Không cần thay đổi toolchain nào khác: go.mod, Dockerfile đã nhất quán; `go mod verify` sạch; gofmt/vet/test-race/build đều pass.

## 7. Xác minh các thay đổi đã áp dụng

- Cổng đầy đủ sau khi sửa: gofmt ✓, `go mod verify` ✓, `go vet ./...` ✓, `go test -race ./...` ✓, `go build ./...` ✓.
- Bộ kiểm thử tích hợp bỏ qua sạch sẽ (root-gated) — không bị báo cáo sai là pass.
