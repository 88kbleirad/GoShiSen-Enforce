# Khả năng tái tạo e2e NFQUEUE có đặc quyền — Thiết kế (backlog #6)

**Trạng thái:** đề xuất (triển khai tiếp theo).
**Áp dụng cho:** `scripts/e2e-runner.sh`, `scripts/e2e-env-manifest.sh`, tích hợp job CI
`openwrt-integration`, tài liệu. Phát hành v0.2.1/tài sản/khóa ký
không bị động đến.

---

## 1. Kiểm kê phụ thuộc (những gì e2e cần)

| Phụ thuộc | Ở đâu | Vai trò | Kiểm tra |
| --- | --- | --- | --- |
| root + sudo không cần mật khẩu | host | TAP/netns/loop-mount | `sudo -n true` |
| Hỗ trợ kernel NFQUEUE | kernel VM host | `nfnetlink_queue` proc + module | `/proc/net/netfilter/nfnetlink_queue` |
| netns + TAP + bridge | host | LAN client + phía qemu | `ip netns add`, `ip tuntap` |
| qemu-system-x86_64 | host | VM | `--version` |
| nft / iptables / iproute2 | host | hook + chấp nhận FORWARD của lab | `--version` |
| python3, curl, wget | host | console client, marker, provisioning | `--version` |
| OpenWrt image + kmods | network | boot + NFQUEUE trong VM | SHA256 pins (`image-prep --print-config`) |
| Cổng | host | 5555 console, 2222 ssh fwd, 8088 marker | trống / thuộc về lần chạy này |
| Go | CI/local | build daemon | `go version` |

## 2. Phân loại lỗi (riêng biệt, không bao giờ gộp chung)

| Danh mục | Phát hiện | Báo cáo |
| --- | --- | --- |
| host | thiếu công cụ / không sudo / không có proc NFQUEUE | ENV thoát 3, có thể hành động |
| image-prep | `image-prep` khác không (download/checksum/loop) | PROVISION thoát 4 |
| boot/console | console TCP chưa sẵn sàng trong khung thời gian; serial rác | BOOT thoát 5 |
| sẵn sàng daemon | `NFQUEUE bound` không có trong log daemon trong khung thời gian | BOOT thoát 5 + tail serial/daemon |
| tải lại chính sách | khẳng định e2e trên `policy published`/từ chối | FAIL thoát 1 |
| khẳng định lưu lượng | sai khớp verdict DROP/ACCEPT | FAIL thoát 1 |
| môi trường bị chặn | sandbox không đặc quyền (không sudo hoặc không NFQUEUE) | **BLOCKED** thoát 3 — KHÔNG pass/skip |

## 3. Các bảo đảm tái tạo

- **Một lệnh** (`scripts/e2e-runner.sh`) chạy cùng đường đi trong CI và lab
  có đặc quyền cục bộ; job `openwrt-integration` của CI được thay/thế bởi nó.
- **Manifest** được ghi trước khi chạy: OS/kernel/arch, phiên bản công cụ, sudo,
  capability, module NFQUEUE, pin image/kmod, commit, timestamp.
- **Các artifact được pin**: `openwrt-image-prep.sh --download` xác minh image + kmod
  SHA256 trước khi dùng; nhị phân daemon được build với `-trimpath` và được ghi lại.
- **Bẫy dọn dẹp**: teardown là idempotent, chỉ dùng các đường dẫn pidfile/netns/temp
  đã ghi lại (không bao giờ động đến firewall/trạng thái toàn cục của host ngoại trừ
  FORWARD ACCEPT hẹp của bridge lab, được gỡ khi tắt).
- **Artifact lỗi**: khi chạy khác không, tail serial, log daemon, trạng thái nft,
  proc nfnetlink, module, tiến trình, netns, route, manifest được thu gom vào
  `$E2E_ARTIFACT_DIR/failure-diagnostics.txt`.

## 4. Hành vi sandbox không đặc quyền

Sandbox không đặc quyền (không có sudo không cần mật khẩu, hoặc thiếu NFQUEUE) phải báo cáo
`BLOCKED` (thoát 3) kèm điều kiện tiên quyết thiếu chính xác — không bao giờ pass âm thầm hoặc
skip giả. Job CI có đặc quyền (runner ubuntu-24.04 với sudo + QEMU +
kernel NFQUEUE), vì vậy nó là cổng gating có thẩm quyền.

## 5. Các bài kiểm tra

- thiếu phụ thuộc → thoát 3 + thông điệp;
- không có proc NFQUEUE (mô phỏng) → thoát 3 BLOCKED;
- cổng bị chiếm (5555) → lỗi preflight/boot được xử lý như BOOT, không treo;
- qemu/netns cũ → `lab up` dọn dẹp idempotent trước, chạy lại thành công;
- console sai định dạng (không UTF8) → e2e làm sạch byte (hành vi hiện có), không bao giờ
  kẹt;
- chẩn đoán timeout → thư mục artifact chứa failure-diagnostics;
- chạy lại sau lỗi → lần chạy thứ hai sạch (không sót lại gì).

## 6. Giới hạn

Chỉ QEMU TCG; môi trường có khả năng phần cứng ghi lại siêu dữ liệu riêng nếu được dùng
(không có trong nhiệm vụ này). Không động đến thiết bị production nào.

## 7. Các phát hiện live đã xác minh

- **Preflight thiếu công cụ bắn trước**: PATH không-login của instabox thiếu
  `iptables`; runner thoát 3 và nêu tên công cụ. `need` giờ tìm kiếm toàn bộ
  PATH hệ thống.
- **Cuộc đua udev/partx của image-prep** (`rootfs partition /dev/loopNp2 not found`) là
  nhất thời; runner thử lại một lần sau `losetup -D`.
- **Việc host thiếu NFQUEUE KHÔNG phải vật cản** cho lab QEMU (image VM
  cung cấp kmods; được xác minh bởi checksum image-prep). Manifest ghi lại điều đó; bộ
  suite tích hợp native mới là bộ yêu cầu NFQUEUE host.
- **Workstation không đặc quyền** (không sudo nopasswd) → thoát 3 BLOCKED, đã xác minh.

## 8. Các trạng thái preflight có kiểu

Preflight là một máy trạng thái xác định với lược đồ đầu ra ổn định; cổng
đặc quyền đi trước cổng công cụ, nên không-sudo báo cáo BLOCKED ngay cả khi các công cụ
khác thiếu:

```
[e2e-runner] CONFIG_ERROR: <reason>
[e2e-runner] BLOCKED: <reason>
[e2e-runner] MISSING_DEPENDENCY: <reason>
[e2e-runner] RESOURCE: <reason>
[e2e-runner] READY: privileged e2e preflight passed
```

Các cổng và thứ tự: CONFIG (args/input) → PRIVILEGE (root, ngược lại sudo không cần mật khẩu)
→ DEPENDENCY (curl/wget/python3/qemu/nft/iptables/ip/sudo) → RESOURCE (cổng;
việc host thiếu NFQUEUE là một ghi chú) → READY. Thoát 3 bao trùm toàn bộ lớp môi trường
(tương thích với các caller trước); dòng token là có thẩm quyền.

Khả năng tiêm test (không phụ thuộc công cụ host): `E2E_MOCK_SUDO=no|yes`,
`E2E_MOCK_MISSING_TOOLS="tool …"`, `E2E_MOCK_PORT_OCCUPIED=<port>`,
`E2E_MOCK_CONFIG=bad`, `E2E_PREFLIGHT_ONLY=1` (dừng sau READY).
