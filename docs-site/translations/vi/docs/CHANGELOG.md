# Nhật ký thay đổi (Changelog)

Trạng thái và lịch sử đáng chú ý của GoShiSen-Enforce. Chi tiết theo từng bản phát hành nằm trong `docs/release/` (vd: `v0.2.4-notes.md`, `proposal-next-version-scope.md`).

## Trạng thái phát hành (ảnh chụp từ README, 2026-11-17)

- **v0.2.4** — bản phát hành mới nhất: apk OpenWrt ký production; kho lưu trữ CLI đã ký.
- **v0.2.5** — đã dàn dựng thành bản phát hành DRAFT chờ duyệt của người duy trì: bản vá đóng gói upgrade-hook (issue #57). Phạm vi và checklist chạy phát hành: `docs/release/proposal-next-version-scope.md`.

## Chi tiết đã chuyển ra khỏi README

- Bộ tích hợp: 21 kịch bản netns (e2e, state, NAT, crash, overflow trong `tests/integration/`), gated bằng root, chạy với `sudo make integration`; CI chạy toàn bộ bộ trên runner GitHub (21/21 PASS, không có skip bắt buộc).
- Xác thực OpenWrt: OpenWrt 25.12.6 x86/64 (kernel 6.12.112); dịch vụ tắt mặc định; IPv6 stateful ACCEPT đã xác minh trên kernel 6.12.112 với mẫu quy tắc `ESTABLISHED/RELATED` hai chiều chuẩn — `docs/openwrt.md`, `docs/release/issue-5-investigation.md`.
- Đường nóng không cấp phát (0 allocs/gói); benchmark: `docs/design/performance.md`.
- Danh mục metric (12 họ) và thiết lập Grafana: `docs/release/metrics-grafana-validation.md`.
- Cấu trúc kho: `docs/developer-guide.md`.
