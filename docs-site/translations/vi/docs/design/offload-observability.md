# Khả năng Quan sát Bypass Flowtable — Thiết kế (backlog #3)

**Trạng thái:** đề xuất (triển khai tiếp theo trong nhánh này).
**Áp dụng cho:** đo lường flowtable hit, lệnh `offload observe`, số liệu,
kiểm thử, tài liệu. Bản phát hành v0.2.1/tài sản/khóa ký không bị ảnh hưởng.

---

## 1. Hợp đồng đo lường

Mục tiêu: cung cấp bằng chứng quan sát được về việc bypass flowtable — các
gói đã bỏ qua NFQUEUE — chứ không chỉ sự hiện diện của một flowtable.

- **Có thể quan sát:** một đối tượng flowtable có câu lệnh `counter` (nftables
  đếm các gói được offload của flowtable). nft hiển thị
  `counter packets N bytes M` bên trong khối flowtable. Đây là counter duy nhất
  ở mức kernel trực tiếp đếm các flow được offload.
- **Quan sát phụ trợ:** counter của quy tắc/chuỗi flow-add *trước* so với
  *sau* câu lệnh `flow add @ft`. Các gói chạm vào flowtable được cộng vào
  counter của nó và có thể không xuất hiện trong counter của chuỗi/quy tắc
  hạ lưu.
- **Chỉ đọc:** mọi phép đo đều là `nft -a list ruleset` (chỉ phân tích). Không
  bao giờ sửa đổi trạng thái ruleset/tuyến đường/giao diện/kết nối.

## 2. Mô hình trạng thái (quan sát được, khác biệt với trạng thái phát hiện)

| Trạng thái | Ý nghĩa | Độ tin cậy |
| --- | --- | --- |
| `absent` | Không có flowtable (phát hiện bị tắt) | cao |
| `present_zero_hit` | Flowtable tồn tại, counter ở 0 | cao (có counter) |
| `present_positive_hit` | Flowtable tồn tại, counter > 0 → **đã quan sát bypass** | cao |
| `unknown` | Phát hiện/nft thất bại, hoặc counter không khả dụng (không có câu lệnh `counter`) | giới hạn/không rõ |

**Quy tắc:** zero hit ≠ offload bị tắt. Zero hit chỉ nghĩa là chưa có gói nào
được offload kể từ khi counter được tạo (hoặc lưu lượng không đủ, hoặc flowtable
của nền tảng không offload — ví dụ QEMU TCG). Không bao giờ suy luận "offload
bị tắt" từ counter bằng 0. Báo cáo `unknown`/`limited` kèm lý do khi counter
không khả dụng thay vì đoán.

## 3. Đặt lại counter / tính đơn điệu

- Counter flowtable là lũy kế và đơn điệu trong vòng đời của một flowtable.
- Việc dựng lại flowtable (reload fw4, tắt+bật lại) đặt lại counter về 0.
- `DeltaHits(after, before)`: nếu after ≥ before → delta; nếu after < before
  (đặt lại/quấn vòng) → báo cáo giá trị *after* (dựa trên reset), không bao giờ
  báo cáo delta âm. Điều này được nêu tường minh và ghi lại.
- Việc quấn vòng counter (64-bit) không phải mối lo thực tế nhưng cùng logic
  đặt lại đó bao phủ luôn trường hợp này.

## 4. Bề mặt instrumentation

- `goshisen offload observe` (chỉ đọc):
  - dấu thời gian (UTC RFC3339Nano),
  - trạng thái thực thi (disabled/enabled/failed/unknown),
  - theo từng flowtable: family/table/name, packets/bytes lũy kế,
  - tổng hits + `HitsAvailable`,
  - độ tin cậy (high/limited/unknown) + lý do.
- Số liệu (Prometheus):
  - `goshisen_flowtable_hits_packets` (gauge, lũy kế; -1 khi không khả dụng),
  - `goshisen_flowtable_hits_available` (0/1),
  - `goshisen_flow_offload_state` hiện có.
- Watchdog daemon và lúc khởi động cập nhật các số liệu này từ cùng một bộ
  phân tích; không có thao tác đặc quyền mới.

## 5. Giới hạn nền tảng (tường minh)

- **QEMU TCG:** flowtable tồn tại (fw4) nhưng offload 0 gói — counter hit
  vẫn ở 0. Trường hợp này được ghi nhận là zero-hit, KHÔNG phải "offload bị
  tắt". Bypass không thể quan sát được trong TCG.
- **Phần cứng có khả năng hỗ trợ:** các flowtable làm phẳng offload (một số
  driver) vẫn hiển thị `counter`; hits dương khi đó là bằng chứng bypass trực
  tiếp. Nếu driver bỏ qua hoàn toàn nftables, counter vẫn ở 0 — được báo cáo
  là zero-hit/unknown, không bao giờ là bằng chứng cho bất kỳ điều gì.
- **Thiếu công cụ:** không có nft, bị từ chối quyền, thiếu `ethtool` → `unknown`
  kèm lý do.

## 6. Hồi lùi

Revert PR khôi phục hành vi detect/disable trước đó; các bổ sung số liệu/CLI
mang tính cộng thêm và có thể gỡ bỏ mà không đụng đến ngữ nghĩa thực thi.
Không có thay đổi store. Không đụng đến tag/phát hành.

## 7. An toàn

Phép đo chỉ đọc theo cấu trúc (phân tích `nft -a list ruleset`). Đường dẫn
disable vẫn y hệt như được chuyển giao trong thiết kế offload-disable — thiết
kế này không thêm bất kỳ sự biến đổi nào. Counter zero/giới hạn không bao giờ
tự động kích hoạt thay đổi chính sách.
