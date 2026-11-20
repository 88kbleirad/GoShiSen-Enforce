# Nhật ký thay đổi (Changelog)

Trạng thái và lịch sử đáng chú ý của GoShiSen-Enforce. Chi tiết theo từng bản phát hành nằm trong `docs/release/` (vd: `v0.2.6-notes.md`, `proposal-next-version-scope.md`).

## Trạng thái phát hành (ảnh chụp, 2026-11-19)

- **v0.2.6** — bản phát hành mới nhất (đã công bố 2026-11-19): bản phát hành sửa toolchain S-005 (issue #78) — payload apk và cả hai kho lưu trữ CLI được build với go1.27.4. Ghi chú phát hành và asset: [Releases](https://github.com/88kbleirad/GoShiSen-Enforce/releases/tag/v0.2.6).
- **v0.2.5** — bản phát hành trước (đã công bố 2026-11-17): bản vá đóng gói upgrade-hook (issue #57); payload apk nhúng go1.27.1 (S-005, đã sửa trong v0.2.6).

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

## Hiệu chỉnh (tài liệu hậu v0.2.6, 2026-11-19)

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
