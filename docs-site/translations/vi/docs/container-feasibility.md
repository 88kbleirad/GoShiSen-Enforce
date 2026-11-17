# Đánh giá Khả thi Container

Trạng thái: **HOÃN** — không có image container nào được phát hành trong giai đoạn này.
Một Dockerfile tồn tại (`deployments/docker/Dockerfile`) và ghi lại
hình dạng runtime dự kiến, nhưng một tường lửa đóng gói container tự nó không
bảo vệ lưu lượng máy chủ, và không có runtime container nào sẵn có trong môi trường này
để xác nhận một triển khai trực tiếp.

## Tóm tắt

| Câu hỏi | Trả lời |
|---|---|
| Daemon đóng gói container có bảo vệ lưu lượng máy chủ theo mặc định không? | **Không.** Daemon cài hook nftables của nó (và bind NFQUEUE) trong network namespace nơi nó chạy. Một container trên mạng bridge mặc định bị cô lập trong netns riêng; hook của nó chỉ thấy lưu lượng của chính container. |
| Có thể làm nó bảo vệ lưu lượng máy chủ không? | Chỉ với `--network=host` (chia sẻ netns máy chủ). Khi đó các rule của daemon áp dụng cho toàn bộ máy chủ — một thao tác rủi ro cao phải được xử lý như chạy trực tiếp tường lửa trên máy chủ, không phải như một ranh giới cô lập. |
| Khả thi rootless không? | Không. Việc bind NFQUEUE và nftables yêu cầu `CAP_NET_ADMIN` (và `CAP_NET_RAW`), mà các container rootless không thể có được. |
| Capabilities yêu cầu | `CAP_NET_ADMIN`, `CAP_NET_RAW` (qua `--cap-drop=ALL --cap-add=NET_ADMIN --cap-add=NET_RAW`). Không bao giờ `--privileged`. |
| Yêu cầu kernel | NFQUEUE + conntrack phải được bật trong kernel **máy chủ** ngay cả cho một triển khai container: hệ con netlink không được namespaced cho queue netfilter; cộng với container cần `/proc/net/netfilter/nfnetlink_queue` (scope namespace, do kernel máy chủ cung cấp). |
| Ranh giới cô lập | Container cung cấp cô lập process/filesystem, không phải cô lập mạng cho máy chủ. Luồng netns lab (`scripts/lab.sh`) vẫn là mô hình cô lập đúng để xác nhận. |

## Phân tích

### NFQUEUE từ container
- Daemon dùng `netlink.Dial(unix.NETLINK_NETFILTER)` — một socket,
  không phải thiết bị. Nó hoạt động bên trong netns của container miễn là kernel máy chủ
  có NFQUEUE (`CONFIG_NETFILTER_NETLINK_QUEUE` hoặc module
  `nfnetlink_queue`) và process giữ `CAP_NET_ADMIN`.
- Queue mà nó bind là một tài nguyên toàn cục của kernel được đánh dấu bằng số queue;
  các rule queue được cài qua nftables **trong netns của container**.
  Một rule queue trong netns của container không bắt lưu lượng máy chủ.

### nftables và network namespaces
- Daemon cài bảng của nó trong netns riêng. Với mạng bridge mặc định,
  netns của container chỉ nhận lưu lượng container được forward/NAT'd,
  và chain `forward` của hook (priority filter) chỉ thấy lưu lượng đi qua netns đó.
- `--network=host` đặt daemon trong netns máy chủ. Hook khi đó
  áp dụng cho lưu lượng máy chủ. Điều này biến container thành một lớp đóng gói mỏng
  quanh tường lửa máy chủ — rủi ro triển khai tương đương
  với cài binary trực tiếp trên máy chủ.

### Hành vi mạng máy chủ so với container
- Chế độ máy chủ: daemon thấy mọi lưu lượng forward của máy chủ; đúng cho một
  tường lửa, nhưng không có ranh giới bảo mật giữa daemon và máy chủ.
- Chế độ container: cô lập là thực, nhưng daemon chỉ bảo vệ netns riêng của nó
  — không bảo vệ máy chủ, và không bảo vệ các container khác trừ khi
  mạng được tạo hình sao cho netns của daemon nằm trên đường forwarding
  (một cấu trúc liên kết lab-router).

### Các rủi ro do host networking gây ra
- Một lỗi/cấu hình sai trong container ảnh hưởng đến toàn bộ máy chủ.
- Container phải được chạy với capabilities có giới hạn và rootfs
  chỉ đọc; nếu không, một daemon bị xâm phạm (hoặc host-lib bị xâm phạm)
  giành được quyền kiểm soát netfilter trên toàn máy chủ.

## Khuyến nghị

- Đừng quảng bá image container như một tường lửa máy chủ trong bản
  phát hành này. Ghi lại rằng image OCI dành để triển khai **bên trong** một
  network namespace được cấu hình sẵn (ví dụ netns router lab), không phải
  như một ranh giới bảo vệ máy chủ độc lập.
- Giữ Dockerfile như một tạo phẩm thử nghiệm; triển khai được hỗ trợ chính
  vẫn là binary gốc + systemd unit (xem
  `docs/installation.md`).
- Nếu một image container từng được phát hành, nó phải được xây từ nguồn
  đã tag, được xác nhận runtime trong netns lab với cùng các cổng bảo mật
  như binary gốc, và chỉ được phát hành khi có ủy quyền rõ ràng.
