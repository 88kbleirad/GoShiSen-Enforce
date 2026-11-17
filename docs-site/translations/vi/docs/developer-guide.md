# GoShiSen-Enforce — Hướng dẫn nhà phát triển

## Điều kiện tiên quyết

- Go ≥ 1.25 (khớp `go.mod`; CI và Docker dùng Go 1.25)
- Linux (cho công việc namespace lab sau này, cần root)
- `make` cho các mục tiêu chuẩn

## Build, test, xác minh

```bash
make fmt        # xác minh gofmt sạch (fail khi có tệp chưa định dạng)
make vet        # go vet ./...
make test       # go test ./...
make build      # build bin/goshisen với ldflags phiên bản
make check      # fmt + vet + test + build (gate cục bộ đầy đủ)
make run        # build và chạy với configs/goshisen.yaml
make clean      # xoá bin/
make tidy       # go mod tidy
make package     # build mọi kho lưu trữ phát hành (amd64+arm64) + checksums + SBOM
make verify-package  # xác thực các kho lưu trữ: arch, liên kết tĩnh, phiên bản, checksums, SBOM
make sbom       # (tái)tạo SBOM SPDX (cần syft)
make help       # liệt kê mọi mục tiêu
```

CI (`.github/workflows/ci.yml`) chạy cùng gate trên Ubuntu: `go mod verify`, kiểm tra gofmt, `go vet`, `go test -race`, `go build`, và smoke test CLI. Job `package` ngoài ra build + xác minh các kho lưu trữ phát hành trên mọi PR (artifact được tải lên để xem xét, không bao giờ công bố). Workflow `release` (`.github/workflows/release.yml`) chỉ chạy trên thẻ `v*`: nó build + xác minh và gắn artifact vào bản phát hành **draft** cho thẻ — việc công bố vẫn là bước thủ công, có uỷ quyền.

## Cấu trúc kho

```
cmd/goshisen/        package main (dispatch CLI mỏng)
internal/app/        vòng đời daemon: nạp chính sách, cài hook, vòng queue, API/metrics, tắt máy
internal/cli/        phân tích cờ, help/version, xác thực lệnh, cờ lệnh con policy
internal/config/     mô hình cấu hình, giải mã YAML (strict), gộp ưu tiên, redaction, kiểm tra máy chủ chỉ-đọc
internal/policy/     parser chính sách YAML, snapshot bất biến, thay thế nguyên tử, engine có trạng thái
internal/packet/     backend NFQUEUE (nfnetlink_queue trực tiếp qua mdlayher/netlink) + parser IPv4/TCP/UDP
internal/hook/       cung cấp hook nftables (nhị phân nft; chỉ namespace hiện tại)
internal/store/      kho chính sách phiên bản bền vững (con trỏ active nguyên tử)
internal/api/        REST API (localhost, bearer auth, version CAS, audit, liveness/readiness)
internal/metrics/    registry Prometheus bản số giới hạn
configs/             cấu hình mẫu
policies/default/    ví dụ chính sách deny-all
policies/examples/   ví dụ chính sách đã làm việc
api/openapi.yaml     đặc tả OpenAPI khớp triển khai API
scripts/lab.sh       lab netns cô lập (up/down/status)
tests/integration/   bộ tích hợp lưu lượng thật + probe helper (gated bằng root)
docs/design/         architecture.md, mvp-scope.md (thiết kế quy phạm)
docs/                hướng dẫn người dùng, hướng dẫn này
.github/workflows/   CI (build + package + job tích hợp đặc quyền); release (trigger theo thẻ, chỉ draft)
```

NAT nằm trong `internal/hook` (chain nftables phía kernel trong bảng do ứng dụng sở hữu); `internal/nat` không tồn tại — NAT không có package riêng. Package hook render toàn bộ ruleset (filter + NAT) như một giao dịch `nft -f` nguyên tử duy nhất.

## Quy ước

- **Không tiến bộ giả.** Một package chỉ tồn tại khi nó có mã và test thật. Daemon không bao giờ tuyên bố tính năng nó không triển khai.
- **Không sửa đổi hệ thống ngoài phase sở hữu.** Package hook chỉ sửa đổi `table ip goshisen` trong namespace hiện tại (`nft`); tường lửa máy chủ không bao giờ được liệt kê, flush, hoặc sửa. Trạng thái nftables của các lab nằm trong `gsh_fw` và biến mất cùng namespace của nó.
- **Mọi gói đều nhận verdict.** Backend NFQUEUE luôn gửi ACCEPT hoặc DROP; bất thường (lỗi parse, panic, thiếu decider) đều là DROP.
- **stdlib-first.** Phụ thuộc ngoài có chủ đích; hiện chỉ `gopkg.in/yaml.v3` và `github.com/mdlayher/netlink` (đã ghim).
- **Ưu tiên tường minh.** Lớp cấu hình: defaults < file < environment < flags. Mỗi trường ghi nguồn của nó.
- **Parse nghiêm ngặt.** Giải mã YAML dùng `KnownFields(true)`; lỗi đánh máy là lỗi nạp.
- **Lỗi rõ ràng.** Lỗi hướng tới người dùng ghi tên trường, giá trị vi phạm, và cách sửa.
- **Tín hiệu.** SIGINT/SIGTERM dừng hàng đợi và thoát 0; hook vẫn ở nguyên (fail-closed).

## Kiểm thử

- `internal/config`: xếp lớp ưu tiên, YAML strict, phân tích boolean env, lỗi xác thực, tóm tắt nguồn, redaction, mở rộng `~`, `queue_maxlen` (default/file/env/flag + biên).
- `internal/cli`: help/version, phân tích cờ, xác thực lệnh con, phân tích đích `policy simulate`, `--queue-maxlen`.
- `internal/app`: logger text/JSON, lọc mức, vòng đời với fake queue (policy-trước-hook, đóng máy mượt, lỗi mở queue), watcher tràn hàng đợi (delta, cảnh báo giới hạn tần suất, thoát khi stats không có), công bố nguyên tử (`publish` theo thứ tự, malformed/thất bại một phần giữ chính sách cũ, công bố đồng thời tuần tự thành phiên bản riêng, nạp lại IPv4/IPv6, SIGHUP đầu-cuối khi đang chạy), `-race` sạch.
- `internal/policy`: biên CIDR, biên cổng, quy tắc chồng lấn/thứ tự, deny mặc định, cấu hình không hợp lệ, bất biến snapshot, thay thế nguyên tử, đồng thời, ngữ nghĩa có trạng thái (accepted-set, five-tuple-không-đủ, reload-giữ-established).
- `internal/packet`: parser (TCP/UDP hợp lệ, options, cắt ngắn, fragment, giao thức không hỗ trợ, quét no-panic); verdict `evaluate` backend (accept/drop/parse-error/panic); ánh xạ `applyCTInfo`; bộ đếm; parser thống kê queue kernel (`qstats.go`); bất biến fail-closed (không bao giờ yêu cầu `NFQA_CFG_F_FAIL_OPEN`).
- Fuzz `internal/packet`: `make fuzz` (30s) — `FuzzParse` sạch.
- `internal/store`: xác thực phiên bản, ghi nguyên tử (không có tệp tạm lạc), toàn vẹn con trỏ active.
- `internal/api`: auth (thiếu/hợp lệ + audit + không rò token), chính sách không hợp lệ giữ active, xung đột phiên bản, áp dụng đồng thời (đúng một 201), phục hồi khởi động lại, activate/rollback, giới hạn body, readiness, metrics, metric kết quả policy-update, đồng thời shared-mutex với công bố daemon, phục hồi kill giữa publish (phiên bản rách không bao giờ kích hoạt); tất cả dưới `-race`.
- `internal/metrics`: bản số label giới hạn, không có label bí mật/được suy từ flow, đầu ra histogram/gauge/counter, counter tràn hàng đợi (enum `cause` giới hạn).
- `tests/integration`: kịch bản lưu lượng thật (enforcement 6, stateful 6, NAT 5, overflow 1). **Gated bằng root**: `checkPrereqs` skip với lý do tường minh. Chạy với `sudo make integration` (CI có job đặc quyền riêng).
- Smoke test CLI chạy trong CI (help/version, lỗi thiếu config, lỗi chính sách không hợp lệ).

## Phiên bản

`make build` đóng dấu `internal/cli.Version` và `internal/app.Version` bằng `git describe --tags --always --dirty`. Build chưa gắn thẻ báo `dev`.

## Chính sách

- Chạy `make check` trước khi commit; CI chạy cùng gate.
- Test phải qua với `go test -race ./...` (CI làm điều này).
- `gofmt` được thực thi; chạy `make fmt-fix` khi `make fmt` fail.
