# Phát hiện + Vô hiệu hóa Offload — Thiết kế (backlog #2)

**Trạng thái:** đề xuất (triển khai tiếp theo trong nhánh này).
**Áp dụng cho:** xử lý flow-offload của daemon, lệnh con CLI `offload`,
cấu hình `offload_policy`, script init OpenWrt, tài liệu. Tài sản phát hành
v0.2.1 không bị động đến.

---

## 1. Hợp đồng thực thi chính sách

GoShiSen-Enforce thực thi chính sách tại thời điểm NFQUEUE trên đường forward của router.
Một gói mà kernel xử lý **không** đi qua chuỗi forward và queue
không bao giờ nhận verdict — đó là một lỗ hổng thực thi.

| Cơ chế | Có né NFQUEUE? | Quan sát được từ Linux? | Xử lý |
| --- | --- | --- | --- |
| Quy tắc nftables flowtable + `flow offload`/`flow add @` | **có** (các flow established bỏ qua chuỗi) | có — `nft list flowtables`, `nft -a list ruleset` | phát hiện → vô hiệu hóa tường minh (đã xác minh) → fail closed khi còn tồn tại |
| Flowtable với cờ `offload`/hardware offload | **có** (cùng fast-path, driver offload) | có — cờ flowtable (`offload`) | giống trên; né ở mức driver ngoài nft là *không rõ* (bên dưới) |
| Gia tốc flow L4 của driver/nền tảng (tính năng ethtool, bộ phân loại HW) | có thể (tùy nhà cung cấp) | thường không qua netlink | chỉ kiểm kê; được ghi nhận là giới hạn **không rõ**; không thể vô hiệu hóa một cách tổng quát |
| bridge/netfilter (`net.bridge.bridge-nf-call-iptables`) | không (gói vẫn đi qua chuỗi forward của chúng ta; có thể đánh giá hai lần) | có — sysctl | chỉ kiểm kê; không phải né |
| Software offload trên loopback / veth | không (không có flowtable) | n/a | n/a |

**Hợp đồng:** các gói *phải* được kiểm tra chính sách là: mọi TCP/UDP được forward
có trạng thái (NEW/ESTABLISHED/RELATED). Khi một offload có thể né điều đó, daemon
phải có khả năng nói **chính xác** nó đang ở trạng thái nào: disabled, enabled,
failed (đã thử vô hiệu hóa nhưng vẫn enabled), hoặc unknown. Việc thực thi không được
âm thầm tiếp tục trong trạng thái có thể bị né.

## 2. Trạng thái phát hiện

`FlowOffloadReport` có thêm một trạng thái + chi tiết:

- `disabled` — không có đối tượng flowtable và không có quy tắc flow-offload nào trong
  ruleset. Không có lỗ hổng thực thi.
- `enabled` — có ít nhất một flowtable với hook, hoặc một quy tắc thực hiện
  `flow offload`/`flow add @`. Chi tiết liệt kê vị trí (table/chain/flowtable).
- `unknown` — bản thân việc phát hiện thất bại (thiếu nft, lỗi liệt kê). Daemon
  coi unknown là **không an toàn để thực thi** (chính sách `enforce` fail closed).
  (Lỗi phát hiện được ghi log; trạng thái không bị đoán.)
- `failed` — đã thử vô hiệu hóa và phát hiện lại vẫn báo cáo `enabled`
  (việc vô hiệu hóa không có tác dụng). Được dùng bởi đường vô hiệu hóa và nhánh
  fail-closed của chế độ `enforce`.

Các lệnh kiểm kê được ghi lại theo môi trường (bằng chứng): `nft list flowtables`,
`nft -a list ruleset` (grep `flow`), `uci show firewall | grep -E 'flow|offload'`,
`ethtool -k <if>`, `sysctl -a | grep -E 'bridge-nf|flow'`, cùng kernel/target/
driver/interface/timestamp. **Việc QEMU TCG không có flowtable KHÔNG phải bằng chứng
hardware offload đang tắt** — TCG không có engine offload; sự vắng mặt chỉ chứng minh
ruleset không có flowtable nào.

## 3. Đường vô hiệu hóa (tường minh, idempotent, đã xác minh)

Hai hành động phối hợp:

1. **Người vận hành nhìn thấy, bền vững (OpenWrt):** đặt UCI
   `firewall.@defaults[0].flow_offloading='0'` và tải lại firewall4
   (`/etc/init.d/firewall restart`). Đây là sửa chữa lâu dài; fw4 sẽ không
   tạo lại flowtable. Được ghi chép, kèm hồi lùi (đặt `1` + tải lại).
2. **Daemon/CLI tức thời, đã xác minh (`goshisen offload disable`):** thực hiện một
   chương trình `nft -f` mang tính transaction, chỉ gỡ các đối tượng flow-offload:
   - `nft delete flowtable <fam> <table> <flowtable>` cho mỗi flowtable tìm thấy;
   - `nft delete rule <fam> <table> <chain> handle <N>` cho mỗi quy tắc có
     câu lệnh là `flow offload`/`flow add @` (handle từ `nft -a list
     ruleset`).
   Không động đến gì khác (không xóa table/chain, không có bề mặt input/quản lý).
   Idempotent: nếu không tìm thấy gì, thành công no-op. Sau khi xóa,
   phát hiện lại; nếu vẫn `enabled` → báo cáo `failed` và thoát với mã khác không.

Thứ tự: phát hiện chạy lúc daemon khởi động **sau** quyết định cài hook nhưng
**trước** readiness; watchdog 10 giây chạy lại phát hiện, vì vậy một offload
được kích hoạt bởi lần tải lại firewall sau đó sẽ bị bắt và (trong chế độ `enforce`)
được thử lại / fail closed.

## 4. Ma trận hành vi (cấu hình `offload_policy`)

| Chính sách | offload `disabled` | offload `enabled` | offload `unknown` |
| --- | --- | --- | --- |
| `warn` (mặc định) | chạy, ready | chạy, warn + gauge, ready | chạy, warn |
| `refuse` | chạy, ready | **khởi động thất bại trước khi cài hook** | chạy (warn) — unknown không phải lỗ hổng đã chứng minh |
| `readiness` | chạy, ready | chạy, enforce, gauge, **không bao giờ ready** cho đến khi bị vô hiệu hóa | chạy, ready (warn) |
| `enforce` (mới) | chạy, ready | **thử vô hiệu hóa; nếu xác minh được → chạy+ready, log; nếu vô hiệu hóa thất bại (`failed`) → khởi động thất bại (fail-closed)** | **fail closed** (không thể chứng minh an toàn) |

- `enforce` là chế độ fail-closed mới: nó chủ động đóng lỗ hổng và từ chối
  chạy khi không thể.
- **Cửa thoát hiểm chẩn đoán/phát triển:** cấu hình tường minh `allow_bypass: true`
  (hoặc env `GOSHISEN_ALLOW_BYPASS=1`) hạ `enforce` xuống ngữ nghĩa `warn`
  với một cảnh báo một-lần lớn tiếng. Nó là opt-in, không bao giờ mặc định, và chỉ
  dành cho gỡ lỗi. Không có fallback âm thầm: thiếu nó, enforce không bao giờ tiếp tục
  qua một lỗ hổng.

## 5. Metrics & logging

- Gauge `goshisen_flow_offload_state` (0 disabled, 1 enabled, 2 unknown;
  thay thế boolean `..._active` để có trạng thái phong phú hơn, counter được dẫn xuất).
- Counter `goshisen_flow_offload_disables_total` + `..._failed_total` (kết quả
  vô hiệu hóa, không có label — cardinality bị chặn).
- Các dòng log: chuyển trạng thái (`offload state=enabled detail=...`), mọi
  hành động vô hiệu hóa (`offload disable: removed flowtable ...`), vô hiệu hóa thất bại
  (`offload disable FAILED: ...`), lỗi khởi động fail-closed của enforce.

## 6. Hồi lùi

- Hồi lùi tính năng (revert PR): việc thực thi quay về hành vi warn/refuse/
  readiness trước đó; không lưu trạng thái nào trong kho.
- Hồi lùi vô hiệu hóa: kích hoạt lại qua UCI (`flow_offloading='1'` + tải lại
  firewall); các lần xóa tức thời trong kernel được thay bằng việc tạo lại bởi
  lần tải lại fw4 tiếp theo.
- Nhiệm vụ này không động đến tag/phát hành nào; tài sản v0.2.1 không đổi.

## 7. Giới hạn Hardware/TCG (tường minh)

- QEMU TCG offload 0 gói; một flowtable hiện diện trong ruleset vẫn được
  phát hiện và vô hiệu hóa, nhưng **không có gói nào bị né quan sát được trong lab**.
- Gia tốc flow của driver/nền tảng ngoài nftables không thể quan sát hay
  vô hiệu hóa một cách tổng quát — được ghi nhận là `unknown` cho phần cứng chúng ta không thể
  kiểm tra; `enforce` chỉ fail closed với offload quan sát được qua nft. Đây là
  giới hạn được nêu (backlog #3 vẫn còn).
- Kiểm kê ethtool được ghi lại như bằng chứng, không phải như một kiểm tra.
