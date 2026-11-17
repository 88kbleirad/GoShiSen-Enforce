# Dọn Dẹp Khi Hạ Cấp Gói OpenWrt — Thiết kế (backlog #5)

**Trạng thái:** đề xuất (triển khai tiếp theo).
**Áp dụng cho:** vòng đời gói `goshisen` OpenWrt (`prerm`/`postinst`),
các liên kết rc.d boot-guard, an toàn khi hạ cấp r4→r2. Bản phát hành
v0.2.1/tài sản/khóa ký không bị ảnh hưởng.

---

## 1. Kiểm toán (chủ sở hữu / đường dẫn / hành động)

| Đường dẫn | Chủ sở hữu | Được tạo bởi | Hành động khi gỡ cài/hạ cấp |
| --- | --- | --- | --- |
| `/etc/init.d/goshisen` | r2+r4 (tệp gói) | cài đặt | bị gỡ bởi apk (tệp gói) |
| `/etc/init.d/goshisen-boot` | **chỉ r4** | cài đặt | bị gỡ bởi apk khi r4→r2 (không có trong r2) |
| `/etc/rc.d/S90goshisen` | gói (runtime) | postinst `goshisen enable` (rc.common) | phải bị gỡ bởi prerm (prerm r2 chỉ `stop` — **lỗ hổng**) |
| `/etc/rc.d/K10goshisen` | gói (runtime) | như trên | phải bị gỡ bởi prerm |
| `/etc/rc.d/S12goshisen-boot` | gói (runtime) | postinst r4 `goshisen-boot enable` | **phải bị gỡ bởi prerm r4** — hiện bị chặn bởi `[ -x /etc/init.d/goshisen-boot ]` có thể sai trong lúc hạ cấp → **treo lơ lửng** |
| `/etc/rc.d/K10goshisen-boot` | gói (runtime) | như trên | như trên |
| `/etc/config/goshisen`, `/etc/goshisen/policy.yaml`, `/usr/sbin/goshisen` | gói | cài đặt | bị gỡ bởi apk |

Nguyên nhân gốc: `prerm` của r4 gỡ các liên kết rc.d **chỉ khi**
`/etc/init.d/goshisen-boot` vẫn tồn tại (guard `-x`). Trong lúc hạ cấp r4→r2,
apk có thể gỡ tệp init đó trước khi chạy `pre-deinstall`, nên các symlink rc.d
vẫn còn và trỏ tới một đích không tồn tại (treo lơ lửng). `prerm` của r2 cũng
không bao giờ chạy `disable` cho `/etc/init.d/goshisen`, nên `S90goshisen`/`K10goshisen`
cũng có thể treo lơ lửng sau khi gỡ cài r2.

## 2. Hợp đồng dọn dẹp

- **Các liên kết rc.d do gói sở hữu là tất định** (tên bắt nguồn từ `START=`/`STOP=`
  của init script): `S12goshisen-boot`, `K10goshisen-boot`, `S90goshisen`, `K10goshisen`.
- Khi **bất kỳ** lần gỡ bỏ gói nào (gỡ cài hoặc hạ cấp xuống r2), `prerm` PHẢI
  gỡ cả bốn liên kết một cách **vô điều kiện** trong `/etc/rc.d/` — không bao giờ
  được chặn bởi sự tồn tại của init script đích, không bao giờ bám theo symlink
  (`unlink`, không phải `rm -rf`), không bao giờ đụng đến bất kỳ mục rc.d nào khác.
- Các init **script** do apk tự gỡ (chúng là tệp gói); các liên kết là trạng thái
  runtime và phải được dọn bởi script.
- `unlink` thất bại trên một liên kết không tồn tại là thành công (idempotent);
  thất bại trên một mục *không phải symlink* cùng tên (ví dụ một tệp thật do gói
  khác để lại) là một lỗi → hủy bỏ kèm thông báo rõ ràng (không giả định quyền
  sở hữu đường dẫn).
- `hook cleanup` (gỡ bảng nftables) giữ nguyên như hiện tại, được chặn bởi `-x`
  trên nhị phân (chỉ khi nhị phân daemon tồn tại).

## 3. Thứ tự (fail-safe)

Thứ tự `prerm`: (1) tắt/dừng các dịch vụ tồn tại, (2) **unlink vô điều kiện
các liên kết rc.d**, (3) hook cleanup. Điều này bảo đảm ngay cả khi các tệp init
đã biến mất, trạng thái rc.d vẫn nhất quán vào lúc việc gỡ tệp gói hoàn tất.
Boot guard bị tắt trước khi bất kỳ sự hoán đổi nhị phân nào, vì pre-deinstall
của apk chạy trước khi các tệp của gói mới được cài.

## 4. Metadata thiếu/hỏng

- Nếu liên kết rc.d tồn tại nhưng `readlink` thất bại (hỏng), `unlink` vẫn gỡ nó.
- Nếu một liên kết trỏ đi nơi khác (không phải `../init.d/...`), nó không phải
  của chúng ta để gỡ → xác minh tiền tố đích trước khi unlink (chỉ
  `../init.d/goshisen*`).
- Nếu `prerm` không thể xác minh một đường dẫn (quyền truy cập), thất bại với
  một thông báo tường minh; KHÔNG trả về thành công (không thành công giả).

## 5. Hành vi apk thực nghiệm (đã xác minh trực tiếp, OpenWrt 25.12.6 QEMU)

- **apk v3 KHÔNG chạy pre-deinstall của gói cũ khi hạ cấp.** Một lần
  `apk add --no-network --allow-untrusted <r2.apk>` sạch trên r4 để lại cả bốn
  liên kết rc.d nguyên vẹn trong khi `/etc/init.d/goshisen-boot` bị gỡ → S12/K10
  `goshisen-boot` treo lơ lửng. Do đó bản sửa prerm r4 không thể kích hoạt khi hạ
  cấp; nó kích hoạt khi **gỡ cài** (đã xác minh: `apk del` sau khi cài r4 đã sửa
  gỡ cả bốn liên kết, `NO-LINKS`).
- **`goshisen-boot disable` trước khi hạ cấp là fail-safe đã được xác minh**:
  nó gỡ S12/K10 trong khi init script đích vẫn tồn tại; sau khi hạ cấp không còn
  liên kết boot-guard nào (chỉ còn S90/K10 goshisen, thuộc sở hữu của r2).
- **Giải cứu sau hạ cấp**: lệnh `disable` và thư viện vòng đời đều đã biến mất
  (r2 không vận chuyển chúng). Giải cứu tất định là
  `unlink /etc/rc.d/S12goshisen-boot /etc/rc.d/K10goshisen-boot`
  (đường dẫn cố định, không bám theo, không xóa đệ quy). Điều này được ghi lại.

## 6. Kiểm thử

Mức shell (kho `tests/package/`): cài đặt→các liên kết hiện diện; nâng cấp
r2→r4→các liên kết hiện diện; hạ cấp r4→r2→các liên kết biến mất; gỡ cài→các
liên kết biến mất; dọn dẹp lặp lại idempotent; đích thiếu; symlink sai dạng;
tiền tố đích lệch (rc.d ngoại lai không bị đụng); guard điều hướng đường dẫn
(không có `../../`).

OpenWrt QEMU trực tiếp (apk r2/r4): baseline r2, nâng cấp, reboot/boot guard,
hạ cấp, reboot, kiểm tra phiên bản, gỡ cài, bằng chứng kiểm kê — đã xác minh
như trên.

## 7. Giới hạn QEMU/phần cứng

Thứ tự boot/reboot được quan sát dưới QEMU TCG; thời điểm boot trên phần cứng
không được khẳng định (cùng giới hạn như các prompt trước).

## 8. Hồi lùi

Revert PR khôi phục hành vi prerm trước đó; việc dọn dẹp mang tính cộng thêm
và được chặn để chỉ nhắm vào các đường dẫn do gói sở hữu. Không đụng đến
tag/phát hành.
