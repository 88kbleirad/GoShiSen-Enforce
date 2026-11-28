# Cài đặt GoShiSen-Enforce

GoShiSen-Enforce là tường lửa fail-closed, có trạng thái dựa trên
nftables/NFQUEUE. Hướng dẫn này bao gồm việc cài đặt từ nhị phân
phát hành chính thức. Daemon chỉ quản lý bảng nftables của riêng
nó (`ip goshisen`); nó không bao giờ động vào quy tắc tường lửa khác.

## Nền tảng được hỗ trợ

Các tổ hợp sau được build và xác thực trong CI:

| Kiến trúc | Build | Xác thực runtime | Ghi chú |
|---|---|---|---|
| `linux/amd64` | PASS | PASS (CI + cục bộ) | Mục tiêu chính |
| `linux/arm64` | PASS | PASS (CI) | Build liên kết tĩnh; xác thực runtime trên arm64 bị giới hạn bởi mức độ sẵn có của runner arm64 — xem Ghi chú tương thích |

- Cần Go 1.26+ để build từ nguồn: `go.mod` khai báo `go 1.26.0` và ghim
  `toolchain go1.27.4` (nhị phân phát hành đã build sẵn và không cần
  toolchain).
- Daemon là nhị phân tĩnh: không cần thư viện chia sẻ.
- Nhị phân `nft` phải có mặt khi chạy.

### Yêu cầu kernel

Daemon xác minh các điều này lúc khởi động và từ chối chạy (báo
lỗi lớn) nếu thiếu:

- Hỗ trợ NFQUEUE (`CONFIG_NETFILTER_NETLINK_QUEUE`, hoặc module
  `nfnetlink_queue`).
- nftables (`CONFIG_NF_TABLES`).
- conntrack (`CONFIG_NF_CONNTRACK`; các module `nf_conntrack`,
  `nf_conntrack_netlink`).
- Network namespace chỉ cần cho lab tích hợp, không cần cho bản thân
  daemon.
- `/proc/net/netfilter/nfnetlink_queue` (telemetry tràn hàng đợi).

### Tiện ích bắt buộc

- `nft` (nftables userspace) — daemon chạy nó để cài/liệt kê/
  dọn bảng của mình.
- Để xác thực `--interface`, danh sách giao diện mạng của hệ điều hành
  (không cần gói bổ sung).

## Cài đặt từ nhị phân phát hành

1. Tải kho lưu trữ cho kiến trúc của bạn và tệp checksums:

   ```bash
   cd /tmp
   curl -LO https://github.com/88kbleirad/GoShiSen-Enforce/releases/download/v0.1.0/goshisen-linux-amd64.tar.gz
   curl -LO https://github.com/88kbleirad/GoShiSen-Enforce/releases/download/v0.1.0/checksums.txt
   ```

2. Xác minh checksum SHA-256:

   ```bash
   grep goshisen-linux-amd64.tar.gz checksums.txt | sha256sum -c -
   ```

3. Xác minh chữ ký Ed25519 với khóa công khai phát hành:

   ```bash
   curl -LO https://github.com/88kbleirad/GoShiSen-Enforce/releases/download/<TAG>/public_key.pem
   curl -LO https://github.com/88kbleirad/GoShiSen-Enforce/releases/download/<TAG>/goshisen-linux-amd64.tar.gz.sig
   openssl pkeyutl -verify -rawin -pubin -inkey public_key.pem \
     -in goshisen-linux-amd64.tar.gz -sigfile goshisen-linux-amd64.tar.gz.sig
   ```

   Lệnh phải in `Signature Verified Successfully`. Mỗi bản phát hành kèm chính
   khóa đã ký nó dưới dạng asset `public_key.pem` — hãy tải từ bản phát hành bạn
   đang cài, **không** lấy từ `main`, vì anchor có thể được xoay. So sánh vân tay
   khóa một cách độc lập: `openssl pkey -pubin -in public_key.pem -outform DER |
   sha256sum` phải bằng vân tay in trong release notes của bản đó:
   `e9b4bf1194ce90d2377720102748460404eacbc7aba9aa8ad48532cd63294fb9`
   (key ID `bdda690b97a4f49e`; SHA-256 của DER SubjectPublicKeyInfo) cho các bản
   phát hành sau v0.2.9, và
   `24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`
   (key ID `f955ede984245d64`, khóa tạo 2026-11-13) cho v0.2.1–v0.2.9 — anchor
   cũ vẫn được commit tại
   `keys/goshisen-release-2026-11-13.pub.pem`. Xem
   [`docs/release/release-key-rotation-2026-11-28.md`](../../../../docs/release/release-key-rotation-2026-11-28.md).

   Anchor đó **chỉ** là khóa phát hành CLI. Gói OpenWrt được ký bằng khóa khác —
   anchor APK `keys/goshisen-apk.pub.pem` (EC P-256,
   `542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d`), chưa từng
   xoay và là khóa **duy nhất** thuộc `/etc/apk/keys/` — xem
   [`openwrt.md`](openwrt.md) §Signing. Không bao giờ đặt anchor phát hành CLI ở đó.

4. Giải nén và cài đặt:

   ```bash
   tar xzf goshisen-linux-amd64.tar.gz
   sudo install -m 0755 goshisen-linux-amd64/goshisen /usr/local/bin/goshisen
   sudo install -d -m 0755 /etc/goshisen /var/lib/goshisen
   sudo install -m 0644 goshisen-linux-amd64/INSTALL.md /usr/share/doc/goshisen/
   ```

5. Xác minh nhị phân đã cài:

   ```bash
   goshisen --version     # phải in goshisen v0.1.0
   goshisen --help
   ```

## Cấu hình

Một tệp chính sách tối thiểu:

```yaml
version: 1
default_action: DROP
rules:
  - id: allow-ssh
    action: ACCEPT
    protocol: tcp
    dst_port: 22
    state: [NEW]
  - id: allow-estab
    action: ACCEPT
    state: [ESTABLISHED, RELATED]
```

Chạy:

```bash
sudo goshisen --config /etc/goshisen/goshisen.yaml
```

Xem `docs/user-guide.md` để biết tham chiếu cấu hình đầy đủ.

## Capability Linux bắt buộc

Daemon cần:

- `CAP_NET_ADMIN` — quản lý quy tắc nftables và bind NFQUEUE.
- `CAP_NET_RAW` — raw packet socket (NFQUEUE).

Chạy với root hoặc cấp chính xác các capability này:

```bash
sudo setcap cap_net_admin,cap_net_raw+ep /usr/local/bin/goshisen
```

Chạy với người dùng không đặc quyền sau khi nhị phân được cấp các
capability trên. Không chạy với capability rộng hơn.

## Khởi động dịch vụ (systemd)

Một tệp đơn vị được tăng cường bảo mật có sẵn tại `deployments/systemd/goshisen.service`.
Cài nó, tạo tệp chính sách, và khởi động:

```bash
sudo cp deployments/systemd/goshisen.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now goshisen
```

Dịch vụ dùng `Restart=always` — sau crash daemon khởi động lại
và cài lại hook của nó (fail-closed theo thiết kế).

## Kích hoạt tường lửa an toàn

Daemon fail-closed theo mặc định: chính sách rỗng hoặc daemon chưa
khởi động sẽ DROP mọi lưu lượng. Để kích hoạt an toàn:

1. Bắt đầu với chính sách cho phép trước (vd: ACCEPT tất cả) và xác
   minh API truy cập được, sau đó áp dụng chính sách hạn chế qua API
   (`PUT /v1/policy`) hoặc tải lại.
2. Khi có lỗi bất kỳ, chính sách trước vẫn hoạt động; daemon không bao
   giờ âm thầm fail open.

## Nâng cấp

1. Tải và xác minh nhị phân phát hành mới như trên.
2. Dừng dịch vụ: `sudo systemctl stop goshisen`.
3. Thay nhị phân: `sudo install -m 0755 ... /usr/local/bin/goshisen`.
4. Khởi động và xác minh: `sudo systemctl start goshisen`; kiểm tra
   `goshisen --version` và API health.

Kho chính sách bền vững (`/var/lib/goshisen`) sống sót qua nâng cấp;
chính sách đang hoạt động được áp dụng lại khi khởi động lại.

## Hồi lùi

1. Giữ kho lưu trữ phát hành trước (đã xác minh checksum).
2. Dừng, cài nhị phân trước, khởi động — giống nâng cấp.
3. Xác minh đầu ra phiên bản khớp với bản phát hành đã hồi lùi.

## Xử lý sự cố

- **`hook: nft binary not found`** — cài `nftables` (`apt install
  nftables` / `dnf install nftables`).
- **`cannot bind queue: operation not permitted`** — tiến trình thiếu
  `CAP_NET_ADMIN`; cấp capability hoặc chạy với root.
- **`config: interface ... not found`** — tên giao diện đã cấu hình
  sai hoặc không up; dùng `--interface` với giao diện thật.
- **Cảnh báo tràn hàng đợi** — daemon ghi cảnh báo giới hạn tần suất
  khi kernel DROP gói; xem `docs/user-guide.md` để biết cài đặt.
- **Tường lửa có vẻ chặn mọi thứ** — xác minh daemon đang chạy
  (`systemctl status goshisen`), hook đã cài
  (`nft list table ip goshisen`), và hành động mặc định của chính sách
  là ACCEPT nếu đó là ý định.

## Gỡ bỏ an toàn

```bash
sudo systemctl stop goshisen
sudo rm /usr/local/bin/goshisen
sudo rm -rf /etc/goshisen /var/lib/goshisen
sudo systemctl disable goshisen 2>/dev/null || true
sudo rm /etc/systemd/system/goshisen.service
sudo systemctl daemon-reload
```

Hook (bảng nftables) không bị gỡ khi daemon tắt — theo thiết kế
daemon để hook ở nguyên để có hành vi fail-closed. Gỡ nó một cách
tường minh với:

```bash
sudo goshisen hook cleanup
```

## Xác minh thẻ phát hành

Ký thẻ phát hành (SSH `allowed_signers` / GPG) là một **mục tiêu**, chưa
phải yêu cầu đang hiệu lực: nó chỉ trở thành yêu cầu khi người bảo trì
cấu hình khóa ký và commit trust anchor (xem
`docs/release/signed-tags-plan.md`). Cho tới lúc đó, thẻ phát hành là thẻ
annotated nhưng **chưa ký**. Hãy xác minh những gì có thể trước khi
build từ một thẻ:

```bash
git fetch --tags origin
git tag -v vX.Y.Z        # có ý nghĩa khi đã cấu hình ký thẻ
```

Khi đã cấu hình ký, thẻ đã ký in ra `Good "git" signature for
<principal> ...`; thẻ chưa ký in ra `error: no signature found` — đừng
build bản phát hành từ thẻ đó. Gate phía phát hành trong
`scripts/release-pin-gate.sh` chỉ thực thi kiểm tra chữ ký khi được bật
tường minh (`GATE_TAG_SIGN_CHECK=1`); đây không phải yêu cầu đang hiệu
lực cho tới khi người bảo trì cấu hình khóa ký. Quy trình và quản lý
khóa: `docs/release/signed-tags-plan.md`.

## Ghi chú tương thích

- **IPv6 (queue 101):** TCP/UDP IPv6 được forward được xếp hàng trên queue
  `101` (bảng `inet`) và fail closed dưới chính sách DROP — peer portid
  khác 0 trong `/proc/net/netfilter/nfnetlink_queue` nghĩa là socket
  AF_INET6 đã bind. IPv6 ACCEPT có trạng thái yêu cầu **mẫu quy tắc hai
  chiều chuẩn**: cho phép `NEW` cho từng dịch vụ theo một hướng cộng một
  quy tắc ACCEPT `state: [ESTABLISHED, RELATED]` không có src/dst (đã
  xác minh đầu-cuối trên kernel 6.12.112; issue #5). Quy tắc established
  theo hướng cụ thể sẽ DROP ACK/data hướng gốc của client và bắt tay bị
  kẹt — đó là ngữ nghĩa chính sách, không phải lỗ hổng IPv6. Quy tắc
  IPv6 không trạng thái (src/dst/proto/port, không `state`) được hỗ trợ
  và xác minh; xem `docs/openwrt.md` để biết bố trí bảng `inet` phía
  daemon.
- Nhị phân build với `CGO_ENABLED=0`; không phụ thuộc libc cho bản thân
  daemon.
- Nhị phân `nft` userspace phải khớp tập tính năng nftables của kernel
  đích; daemon xác minh quy tắc queue cài được.
- Xác thực runtime arm64 hiện chạy trong CI nơi có sẵn runner arm64; nếu
  bạn dùng arm64 trong production mà không có xác thực CI, hãy chạy
  `sudo make integration` trên máy arm64 trước.
- Không có nền tảng nào khác hiện được build hoặc quảng cáo.
