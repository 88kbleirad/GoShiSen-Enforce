# GoShiSen-Enforce — Ghi chú phát hành v0.2.0 (bản nháp)

## Tóm tắt
v0.2.0 bổ sung phân phối nhị phân Linux tái lập được và đóng gói production
cho daemon tường lửa v0.1.0. Không thay đổi mô hình thực thi tường lửa.

## Mới trong v0.2.0
- **Nhị phân phát hành Linux tái lập được** — `goshisen-linux-amd64.tar.gz`
  và `goshisen-linux-arm64.tar.gz`, liên kết tĩnh (tắt CGO),
  gắn phiên bản, tái lập byte-cho-byte với một cây nguồn cố định
  và phiên bản cố định.
- **Checksums** — `checksums.txt` (SHA-256) cho mọi kho lưu trữ phát hành.
- **SBOM** — `sbom.spdx.json` (SPDX 2.3) tạo bằng syft 1.30.0
  (đã ghim, kiểm tra checksum).
- **Tự động hoá đóng gói** — job `package` của CI build + xác minh các
  kho lưu trữ trên mọi pull request (arch, liên kết tĩnh, nhãn phiên bản,
  checksums, SBOM, tái lập). Workflow `release` chạy trên các thẻ
  `v*` và gắn artifact vào bản phát hành **draft** — việc công bố
  vẫn là bước thủ công, có uỷ quyền.
- **Tài liệu cài đặt** — `docs/installation.md`: nền tảng được hỗ trợ,
  yêu cầu kernel, cài đặt từ nhị phân, xác minh checksum, cấu hình,
  capability, systemd, kích hoạt an toàn, nâng cấp, hồi lùi, xử lý
  sự cố, gỡ bỏ an toàn.
- **Đánh giá khả thi container** — `docs/container-feasibility.md`:
  triển khai container hoá được tài liệu hoá nhưng hoãn lại; hình ảnh OCI
  không được quảng cáo là tường lửa máy chủ.

## Bảo mật
- Hành vi fail-closed không đổi: daemon không bao giờ đặt
  `NFQA_CFG_F_FAIL_OPEN`; mọi đường lỗi đều DROP.
- Các script đóng gói mang tính cộng thêm; chúng không động vào nftables
  hay mô hình tường lửa. Bộ tích hợp đặc quyền vẫn là gate CI bắt
  buộc (21/21 PASS, không skip).

## Giới hạn đã biết
- Nhị phân arm64 được build và xác minh tĩnh; chạy runtime trên arm64 cần
  máy chủ arm64 (đã xác thực ở đó).
- Hình ảnh container không được công bố; xem
  `docs/container-feasibility.md`.
- `nft` (nftables userspace) phải có trên máy đích.

## Kiểm chứng
- CI Go 1.25, ubuntu-24.04: build/vet/race/smoke PASS; job package
  build+verify PASS; tích hợp đặc quyền 21/21 PASS, không skip.
- Cục bộ: `go mod verify`, `gofmt`, `go vet`, `go test -race ./...`,
  kiểm thử hồi quy đóng gói (`tests/package`) PASS.

## Tích hợp OpenWrt (thêm ở phase 10/11)

- **Gói OpenWrt** (`openwrt/`, `.apk` SDK-built): script init procd
  (tắt mặc định; `config_load`, `procd_send_signal` reload, cổng
  enabled), cấu hình UCI, chính sách mặc định fail-closed.
- **Thực thi IPv6**: hook nay là bảng `inet` xếp hàng đợi IPv4
  (queue 100) và IPv6 (queue 101) cho TCP/UDP forward riêng;
  parser và tiền tố chính sách hỗ trợ IPv6. IPv6 không còn vòng qua
  daemon (trước đây là lỗ hổng fail-open). IPv6 ACCEPT có trạng thái
  được xác minh trên kernel 6.12.112 với mẫu quy tắc `ESTABLISHED/RELATED`
  hai chiều chuẩn (issue #5; PR #50); trên kernel không chuyển thuộc
  tính conntrack IPv6, gói sẽ là INVALID → DROP (fail-closed; chưa quan
  sát kernel như vậy, các kernel khác chưa kiểm thử). Quy tắc IPv6
  không trạng thái hoạt động.
- **Phát hiện flow-offload**: daemon phát hiện flowtables nftables /
  `flow add @ft` và cảnh báo lớn (WARN + gauge
  `goshisen_flow_offload_active`, kiểm tra lại mỗi 10 s). Nó không bao
  giờ sửa tường lửa.
- **Quan sát gói sai định dạng**: lỗi backend parse/panic/send được xuất
  ra dưới dạng `goshisen_backend_errors_total{kind=...}`.
- **Build phát hành tĩnh**: `make build` tạo nhị phân liên kết tĩnh, rút
  gọn, tái lập được; CI khẳng định điều đó.
