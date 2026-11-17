# GoShiSen-Enforce — Ghi chú phát hành v0.1.0 (bản nháp)

## Tóm tắt
GoShiSen-Enforce là tường lửa fail-closed, có trạng thái dựa trên nftables/NFQUEUE với API nhúng nhỏ và metrics Prometheus. v0.1.0 là bản phát hành được gắn thẻ đầu tiên.

## Mới trong v0.1.0
- **Sửa giao thức NFQUEUE** — giải mã đúng `NFQA_CT_INFO` 4 byte big-endian, xử lý `ip_conntrack_info` gốc 0, tập accepted chuẩn hoá hướng, và cơ chế chống cắt ngắn `parseAttrs` (trước đây là vector panic; nay đã fuzz).
- **Thực thi fail-closed** — mọi lỗi tràn hàng đợi / hook / parser / engine đều dẫn đến DROP; `NFQA_CFG_F_FAIL_OPEN` không bao giờ được đặt. Không tồn tại đường fail-open.
- **Lọc có trạng thái** — theo dõi kết nối TCP/UDP với bắt tay, xử lý established/related, DROP INVALID, hành vi timeout, và tải lại khi đang established (SIGHUP hoán đổi chính sách nguyên tử; giữ chính sách cũ nếu lỗi).
- **NAT** — DNAT, MASQUERADE, lưu lượng trả về, NAT+lọc, và tải lại khi đang NAT với luồng được conntrack giữ nguyên.
- **Giám sát tràn hàng đợi** — quan sát `queue_dropped` hạt nhân qua `/proc/net/netfilter/nfnetlink_queue`, metric tràn Prometheus, cảnh báo giới hạn tần suất.
- **Củng cố chống crash** — DROP fail-closed khi SIGKILL, phục hồi khi khởi động lại, gating sẵn sàng (không thể fake-ready).
- **Bộ tích hợp đặc quyền** — 21 kịch bản netns thật với đối chứng dương trên mọi đường deny/fail-closed, CI từ chối skip bắt buộc.
- **Căn chỉnh toolchain** — Go 1.25; CI trên `ubuntu-24.04`.

## Bảo mật
- API bind `127.0.0.1` theo mặc định; giới hạn body 16 KiB; thiết lập timeout máy chủ.
- Hook chỉ chạm vào `table ip goshisen`; NAT+lọc áp dụng nguyên tử (một giao dịch `nft -f`).
- Label metric giới hạn; không có bản số IP/cổng/flow-ID.
- Cấu hình mẫu không có thông tin xác thực; `config show` làm mờ.

## Giới hạn đã biết / rủi ro còn lại
- Tích hợp đặc quyền cần kernel có NFQUEUE+conntrack và `CAP_NET_ADMIN`; kernel không hỗ trợ được preflight phát hiện và báo lỗi rõ ràng (không bao giờ bỏ qua im lặng).
- Timeout conntrack là mặc định của kernel; bộ kiểm thử xác minh hành vi mà không chờ hết hạn hoàn toàn.
- Xem `docs/release-checklist.md` §2 để biết gate đầy đủ.

## Kiểm chứng
- CI trên GitHub (ubuntu-24.04, kernel 7.0.0-1017-azure, Go 1.25.14): build/vet/race/build/smoke PASS; tích hợp đặc quyền 21/21 PASS, không có skip bắt buộc.
- Ba mục tồn đọng phase 8.7 nay đã kiểm chứng: NAT-reload (18.47s), queue overflow (23.70s), state timeout/hold (2.82s/1.81s); reload-while-established 18.48s.
- Đối chứng dương trên mọi kịch bản deny/fail-closed đều PASS; không có bài kiểm thử nào bị nới lỏng hoặc bỏ qua để cho xanh.
- Bằng chứng: `docs/release/phase9-ci-validation.md`, `docs/release-checklist.md` §2.

## Hướng dẫn
```bash
make build VERSION=v0.1.0
sudo make integration   # bộ đặc quyền đầy đủ; skip là vật cản
```
Container: thêm `--cap-drop=ALL --cap-add=NET_ADMIN --cap-add=NET_RAW`.
