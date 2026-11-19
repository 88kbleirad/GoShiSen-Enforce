# Tích hợp OpenWrt

GoShiSen-Enforce thêm lớp thực thi NFQUEUE fail-closed **sau**
firewall4 trên đường forward. Nó không bao giờ thay thế firewall4, không
bao giờ sửa bảng của fw4, và không bao giờ NAT.

Trạng thái: đã xác thực trên OpenWrt 25.12.6 (x86/64, kernel 6.12.112) trong QEMU.
Runtime x86_64 đã xác minh đầy đủ; aarch64 (armsr/armv8) được build và
xác minh APK nhưng lab runtime QEMU trực tiếp của nó bị chặn do môi trường (xem
`docs/release/openwrt-v022-package-validation.md` §6); các kiến trúc khác
chưa được kiểm thử.

Bản phát hành **r5 (0.2.0-r5, pin `c5de6e8` = v0.2.2)** đã xác minh đầu-cuối:
cài mới, tắt-mặc định, bật/khởi động, queues 100+101,
đối chứng DROP/ACCEPT, gỡ offload `enforce` (đã xác minh), vòng đời
(stop/start/restart/reload), kill -9 fail-closed + respawn, khởi động lại với
boot guard, nâng cấp r4→r5 (giữ cấu hình), hạ cấp r5→r4 (không có
liên kết guard lủng lẳng), dọn khi gỡ cài — trên x86_64 và aarch64
(QEMU armsr/armv8 sống, xem `docs/release/arm64-runtime-validation.md`).
Xem `docs/release/openwrt-v022-package-validation.md`.

Lưu ý hạ cấp r5→r4: daemon r4 (nhị phân thời v0.2.1, pin
`f5f211a`) không hiểu trường YAML `allow_bypass` mà script init r5
render. Sau khi hạ cấp, **khởi động lại dịch vụ một lần**
(`/etc/init.d/goshisen restart`) để init r4 render lại cấu hình runtime;
nếu một `/var/run/goshisen/config.yaml` do r5 render bị đọc trước,
daemon r4 từ chối khởi động (xoá tệp đó là cách thay thế sạch). Đường
hạ cấp đã xác minh trong báo cáo dùng quy trình này.

Bản phát hành r4 (0.2.0-r4, pin `f5f211a`) đã xác minh đầu-cuối trước đó:
nâng cấp từ v0.2.0-r2, tương thích cấu hình cũ, thứ tự boot guard (S12),
các chế độ chính sách flow-offload (warn/refuse/readiness), IPv6
có trạng thái (allow/deny/established replies với peer v6 thật), tính
nguyên tử khi reload lỗi, phục hồi crash/restart, và hồi lùi về r2. Xem
`docs/release/openwrt-compat.md`. Lưu ý: hồi lùi từ r4 về
r2 để lại symlink rc.d `S12goshisen-boot`/`K10goshisen-boot` lủng lẳng
(r2 không kèm guard). **Trước khi hạ cấp**, hãy tắt guard để các liên kết
rc.d của nó bị gỡ trong khi script vẫn tồn tại:

```sh
/etc/init.d/goshisen-boot disable   # gỡ S12/K10goshisen-boot
apk add --allow-untrusted goshisen-0.2.0-r2.apk
```

apk không chạy pre-deinstall của gói *cũ* khi hạ cấp, nên việc dọn
không thể tự động hoá từ phía r4; `disable` trước-hạ-cấp là đường được
hỗ trợ và đã xác minh. Nếu hạ cấp đã xảy ra mà không làm vậy, hãy gỡ
trực tiếp hai liên kết lủng lẳng (đường dẫn cố định, không xoá đệ quy):

```sh
unlink /etc/rc.d/S12goshisen-boot /etc/rc.d/K10goshisen-boot
```

Gỡ cài (`apk del goshisen`) an toàn: package prerm gỡ cả bốn liên kết
rc.d (đã xác minh).

## Cài đặt (gói đã ký, v0.2.3+)

Kể từ v0.2.3 gói OpenWrt (`goshisen-0.2.3-r1.apk`) được
**ký production** (apk v3). Cài anchor công khai và gói mà không cần
`--allow-untrusted`:

```sh
# trên router/VM (với root)
mkdir -p /etc/apk/keys
wget -O /etc/apk/keys/goshisen-apk.pub.pem \
  https://raw.githubusercontent.com/88kbleirad/GoShiSen-Enforce/v0.2.3/keys/goshisen-apk.pub.pem
openssl pkey -pubin -in /etc/apk/keys/goshisen-apk.pub.pem -outform DER | sha256sum
# phải in: 542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d
wget -O /tmp/goshisen.apk \
  https://github.com/88kbleirad/GoShiSen-Enforce/releases/download/v0.2.3/goshisen-0.2.3-r1.apk
sha256sum /tmp/goshisen.apk    # 948e2e1de454c159bed81bfe845bad160dcf9454238b532b2a87f2e335b0cc6a
apk add /tmp/goshisen.apk       # apk xác minh chữ ký; không cần --allow-untrusted
```

Anchor APK là khóa công khai EC P-256
(`keys/goshisen-apk.pub.pem`, vân tay `542bb7ae…`) và **không phải**
khóa phát hành CLI (asset `public_key.pem` của phát hành, Ed25519,
vân tay `24215e01…`, chỉ xác minh các tarball/checksums/SBOM).
Gói cài đặt tắt mặc định; bật bằng `uci set
goshisen.main.enabled=1; uci commit goshisen; /etc/init.d/goshisen
enable && start`.

Xác minh chữ ký APK do trình quản lý gói thực thi: không có anchor,
`apk add` gói đã ký fail closed (UNTRUSTED).
Đường chuyển tiếp `--allow-untrusted` cũ đã bị gỡ cùng bản phát hành
ký (v0.2.3).

Gói cài đặt:

| Đường dẫn | Mục đích |
| --- | --- |
| `/usr/sbin/goshisen` | daemon tĩnh (CGO off) |
| `/etc/init.d/goshisen` | script init procd |
| `/etc/config/goshisen` | cấu hình UCI (tắt mặc định) |
| `/etc/goshisen/policy.yaml` | chính sách mặc định fail-closed (`DROP`, không quy tắc) |

Phụ thuộc: `kmod-nfnetlink-queue`, `kmod-nft-queue`,
`kmod-nf-conntrack`, `kmod-nf-conntrack-netlink`, `nftables`.

**Dịch vụ tắt mặc định.** Cài gói không bật nó và không động vào tường
lửa.

## Cấu hình UCI (`/etc/config/goshisen`)

| Tùy chọn | Mặc định | Ý nghĩa |
| --- | --- | --- |
| `enabled` | `0` | công tắc chính; daemon chỉ khởi động khi `1` |
| `queue_num` | `100` | số NFQUEUE cho IPv4 (IPv6 dùng `queue_num+1`) |
| `hook_scope` | `openwrt` | `openwrt` = hook sau fw4 (ưu tiên `filter + 1`); `forward-all` = hook forward drop độc lập |
| `log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `interface` | `br-lan` | netdev kernel dùng xác thực máy chủ (tên zone fw4 như `lan` bị từ chối) |
| `policy_file` | `/etc/goshisen/policy.yaml` | chính sách YAML |
| `api_port` | `0`* | REST API (chỉ loopback). *Lưu ý: trong v0.2.6 giá trị `0` từ tệp/cờ bị bỏ qua và quay về 8080. Để thực sự tắt, giữ dịch vụ tắt (`enabled=0`) hoặc đặt `GOSHISEN_API_PORT=0` trong môi trường. |
| `metrics_port` | `0`* | metrics Prometheus (chỉ loopback). *Tương tự: `0` từ tệp/cờ quay về 9090. Tắt qua `GOSHISEN_METRICS_PORT=0` trong môi trường. |
| `queue_maxlen` | `1024` | độ dài hàng đợi kernel |
| `offload_policy` | `warn` | phản hồi flow-offload: `warn` (log WARN + gauge), `refuse` (khởi động fail trước khi cài hook khi phát hiện offloading), `readiness` (thực thi nhưng không bao giờ sẵn sàng trong khi offloading hoạt động), `enforce` (chủ động tắt offloading và xác minh; fail closed nếu tắt thất bại hoặc trạng thái không biết) |
| `allow_bypass` | `0` | cửa sổ chẩn đoán/phát triển, **chỉ** hợp lệ với `offload_policy=enforce`: `1` hạ enforce xuống warn (cảnh báo lớn). Không bao giờ mặc định, không bao giờ im lặng |

Bật và khởi động:

```sh
uci set goshisen.main.enabled='1'
uci commit goshisen
/etc/init.d/goshisen enable
/etc/init.d/goshisen start
```

Cấu hình quy tắc ACCEPT tường minh **trước** khi bật: chính sách đi kèm
DROP mọi thứ.

## Hành vi procd

- `service goshisen start|stop|restart|reload|status` (hoặc script init)
  là các thao tác được hỗ trợ.
- `reload` gửi SIGHUP: chính sách mới được biên dịch trước và chỉ sau đó
  mới hoán đổi; chính sách biên dịch lỗi giữ chính sách trước và ghi
  log `policy reload rejected`.
- `uci commit` **không** khởi động lại dịch vụ (không có vòng lặp restart).
- Dưới procd, stdout/stderr của daemon đi vào syslog (`logread`).
- `respawn` dùng mặc định procd (`threshold 0` = thử lại không giới hạn,
  `timeout 5`, `retry 5`): daemon crash tự động được khởi động lại.

Hành vi boot: script init khởi động tại `START=90` (sau firewall). Daemon
cài hook **trước** khi bind hàng đợi, nên cửa sổ giữa cài hook và bind
queue là fail-closed (gói được xếp hàng không có listener và kernel DROP
chúng). Trước khi daemon khởi động, chỉ chính sách của riêng firewall4
được áp dụng — khoảng trống bị chặn bởi thứ tự khởi động muộn.

## Phạm vi thực thi và khoảng trống

| Mục | Hành vi |
| --- | --- |
| TCP/UDP IPv4 được forward | xếp hàng (queue `100`), chính sách quyết định |
| TCP/UDP IPv6 được forward | xếp hàng (queue `101`; bảng `inet`). Yêu cầu bind AF_INET6 của daemon, ngược lại kernel DROP (fail-closed) |
| Lưu lượng router-local (SSH, LuCI, DNS, DHCP) | không bao giờ xếp hàng: hook chỉ là `forward` |
| Không-TCP/UDP | không xếp hàng; firewall4 quyết định |
| Flow offloading | **được phát hiện và cảnh báo theo mặc định**: luồng established vòng qua queue. Daemon ghi WARN và lộ `goshisen_flow_offload_state` (0 tắt, 1 bật, 2 không biết), kiểm tra lại mỗi 10 s. `offload_policy` điều khiển phản hồi: `refuse` hủy bỏ khởi động trước khi cài hook (fail-closed bằng vắng mặt), `readiness` tiếp tục thực thi nhưng không bao giờ sẵn sàng trong khi offloading hoạt động, `enforce` chủ động tắt offload và xác minh nó, fail closed nếu tắt thất bại hoặc trạng thái không biết (trừ khi đặt cửa sổ chẩn đoán `allow_bypass` tường minh). Khuyến nghị: giữ `flow_offloading` tắt trên router dựa vào thực thi GoShiSen |

Ghi chú IPv6 có trạng thái: IPv6 ACCEPT có trạng thái yêu cầu **mẫu quy
tắc hai chiều chuẩn** — cho phép `NEW` cho từng dịch vụ theo một hướng,
và một quy tắc ACCEPT `state: [ESTABLISHED, RELATED]` riêng không có
src/dst khớp lưu lượng trả lời theo cả hai hướng. Quy tắc established
theo hướng cụ thể (vd: chỉ trả lời `src=... dst=...`)
DROP ACK/data established hướng gốc của client và bắt tay không bao giờ
hoàn tất — đây là ngữ nghĩa chính sách, không phải giới hạn IPv6.
Đã xác minh trên kernel 6.12.112 (OpenWrt 25.12.6 x86/64):
IPv6 có trạng thái hoàn tất đầu-cuối với mẫu chuẩn (xem
`docs/release/issue-5-investigation.md`). Chính sách mặc định đi kèm
(`openwrt/files/etc/goshisen/policy.yaml`) là `default_action: DROP`
không quy tắc. Trên kernel mà bản build `nfnetlink_queue` thực sự
không chuyển thuộc tính conntrack cho IPv6, gói IPv6 đến dưới dạng
INVALID và bị DROP (fail-closed, không bao giờ cho phép) — chưa quan sát
kernel nào ở trạng thái này; các kernel cũ/khác chưa kiểm thử.
Xác minh với `goshisen_packets_by_state_total{state="invalid"}`.

## Phục hồi (LAN→WAN bị chặn)

Hook được cố ý để nguyên khi daemon dừng, nên việc forward vẫn bị chặn
(fail-closed) thay vì được phép im lặng.

Các lựa chọn phục hồi, theo thứ tự:

```sh
# 1. Khởi động/khởi động lại dịch vụ (ưu tiên)
/etc/init.d/goshisen start

# 2. Gỡ hook để chỉ firewall4 điều hành forward
goshisen hook cleanup

# 3. Tắt dịch vụ vĩnh viễn
uci set goshisen.main.enabled='0'; uci commit goshisen
/etc/init.d/goshisen stop
/etc/init.d/goshisen disable
```

Truy cập router-local (serial console, SSH tới router, LuCI) không bao
giờ bị chặn bởi hook forward, nên các lệnh này vẫn truy cập được.

## Nâng cấp và gỡ bỏ

- **Nâng cấp** (`apk add` rN mới hơn): daemon đang chạy giữ chính sách
  trong bộ nhớ; bind queue và hook vẫn ở nguyên, nên không có cửa sổ
  fail-open. Khởi động lại dịch vụ để chạy nhị phân mới. Cấu hình người
  dùng (`/etc/config/goshisen`, `/etc/goshisen/policy.yaml`) được giữ
  như conffile; gói ghi tệp `.apk-new` khi mặc định đi kèm khác.
- **Gỡ bỏ** (`apk del goshisen`): `prerm` chạy `goshisen hook cleanup`
  và dừng dịch vụ, nên bảng do ứng dụng sở hữu bị xoá và forward không
  bị chặn bởi quy tắc queue mồ côi.

## Ký

**Trạng thái: ký gói APK — production-signed từ v0.2.3.**
Workflow phát hành ký các kho lưu trữ CLI phát hành (tarball, checksums,
SBOM) bằng khóa Ed25519 production (vân tay
`24215e016621925215dec0ad6ea14ac74253a4e97249a973e89d649a2217cca0`).
`.apk` OpenWrt được ký production (apk v3) bằng khóa APK có anchor công
khai `keys/goshisen-apk.pub.pem` (EC P-256, vân tay
`542bb7ae9058fee9b4d37ed30b0af44f234ea7cb8866daa1ffc71a7bc37c839d`).
Hai khóa khác nhau và không bao giờ hoán đổi. Cài đặt theo các bước
gói-đã-ký trong phần Cài đặt; `--allow-untrusted` không còn cần thiết
và kiểm tra tin cậy của trình quản lý gói được thực thi.

## Lệnh xác minh

```sh
nft list chain inet goshisen forward        # hook tại ưu tiên filter + 1
cat /proc/net/netfilter/nfnetlink_queue     # queues 100 (v4) và 101 (v6)
logread | grep goshisen                     # khởi động, reload, cảnh báo
wget -qO- http://127.0.0.1:9090/metrics | grep goshisen_   # bộ đếm
```
