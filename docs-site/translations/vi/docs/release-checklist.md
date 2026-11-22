# GoShiSen-Enforce — Checklist phát hành (v0.1.0)

Chạy theo thứ tự. Mọi mục phải PASS trước khi gắn thẻ.

## 1. Gate build từ clean-checkout

- [ ] `git clone` kho mới (cold clone); `go mod verify` PASS.
- [ ] `gofmt -l .` không in gì.
- [ ] `go vet ./...` PASS.
- [ ] `go test -race ./...` PASS (test tích hợp skip khi không-root với lý do tường minh).
- [ ] `go build ./...` PASS.
- [ ] `make check` PASS (fmt + vet + test + build).
- [ ] Smoke CLI: `go build -o /tmp/goshisen ./cmd/goshisen`; `--help`, `--version`; thiếu config thoát khác 0.

## 2. Bộ tích hợp đặc quyền (máy chủ root, trong lab)

- [x] Xác minh điều kiện tiên quyết giờ **có chức năng**: `checkPrereqs` xác minh quy tắc queue nftables cài được trong namespace dùng một lần (NFQUEUE có mặt) và khẳng định `nf_conntrack_count > 0` sau một luồng TCP thật (skip với lý do tường minh nếu không). Reviewer: `tests/integration/e2e_test.go` `checkNFQUEUERule` / `checkConntrackNFQUEUE`.
- [x] **Đối chứng dương** được thêm vào mọi kịch bản deny/fail-closed (DeniedTraffic, DefaultDrop, QueueNoListener, StateUnsolicitedInbound, StateInvalidConnection) để một khẳng định deny không thể PASS nếu daemon/probe hỏng. Đã xác minh PASS trong VM (5/5, exit 0). Bằng chứng: `docs/release/positive-controls-ci.md`.
- [x] `sudo scripts/lab.sh up` thành công; `sudo make integration` chạy toàn bộ bộ (mọi test bắt buộc **phải chạy; skip là vật cản phát hành**):
  - DROP mặc định; allow TCP/UDP tường minh; lưu lượng bị từ chối; ngữ nghĩa khớp-đầu-tiên
  - bắt tay có trạng thái + echo; inbound không được yêu cầu; trạng thái INVALID; timeout/hold; tái dùng UDP
  - nạp lại chính sách khi đang established (SIGHUP nguyên tử; giữ chính sách cũ khi lỗi)
  - DNAT, MASQUERADE, lưu lượng trả về, NAT+lọc, nạp lại khi đang NAT
  - tràn hàng đợi (S11): kernel `queue_dropped` tăng; cảnh báo được ghi; chính sách vẫn được thực thi
  - daemon crash (SIGKILL) → DROP fail-closed; khởi động lại → phục hồi; gating sẵn sàng
  - phạm vi hook cleanup (nftables máy chủ không bị động; bảng ngoài sống sót)
- [x] `sudo scripts/lab.sh down` sạch; `ip netns list` không còn `gsh_*` sót lại.
- [x] Teardown cũng chạy sau khi một test bị kill/hủy bỏ (trap dọn dẹp).

**Ghi chú trạng thái (xác thực CI):** CI trên GitHub (ubuntu-24.04, kernel 7.0.0-1017-azure, Go 1.25.14) xanh: job build PASS, job tích hợp 21/21 PASS với không có skip bắt buộc. Ba kịch bản trước đây bị ràng buộc timeout TCG đã xác minh: NAT-reload (18.47s), queue overflow (23.70s), state timeout/hold (2.82s/1.81s) — cộng reload-while-established (18.48s). Bằng chứng: `docs/release/ci-validation.md`. Draft PR [#1](https://github.com/88kbleirad/GoShiSen-Enforce/pull/1) nhắm main; gate phát hành SẴN SÀNG chờ uỷ quyền merge/tag.

## 3. Kiểm tra bảo mật & củng cố

- [ ] Không token trong argv/log/metrics; `config show` làm mờ; cấu hình mẫu không có thông tin xác thực.
- [ ] API bind 127.0.0.1 theo mặc định; giới hạn body 16 KiB; thiết lập timeout máy chủ.
- [ ] Hook chỉ chạm vào `table ip goshisen`; NAT+lọc áp dụng nguyên tử (một giao dịch `nft -f`).
- [ ] Không đường fail-open: lỗi tràn/queue/hook/parser/engine đều DROP; `NFQA_CFG_F_FAIL_OPEN` không bao giờ đặt.
- [ ] Label metric giới hạn (không IP/cổng/flow-ID); `go mod verify` sạch.

## 4. Nhất quán tài liệu

- [ ] README status, hướng dẫn người dùng, hướng dẫn nhà phát triển, architecture, mvp-scope, SECURITY.md, OpenAPI khớp với triển khai.
- [ ] Phiên bản Go nhất quán: go.mod `go 1.26.0` + `toolchain go1.27.4`, CI/release `go-version: 1.27.4` (với `GOTOOLCHAIN=local`), sàn image Docker, docs `≥ 1.26`.
- [ ] Mọi lệnh được tài liệu hoá chạy được từ gốc kho.

## 5. Cơ chế phát hành

- [ ] Định dạng thẻ: `v0.1.0` (semver). `make build VERSION=v0.1.0` đóng dấu phiên bản. Workflow CI giờ trigger trên thẻ `v*` (thêm trong `f1f09c1`).
- [ ] `git tag v0.1.0 && git push origin v0.1.0` (sau khi bộ đặc quyền PASS).
- [ ] CI trên thẻ chạy xanh (build + tích hợp, không skip bắt buộc).
- [ ] Đóng dấu phiên bản: `make build VERSION=v0.1.0` rồi `bin/goshisen --version` phải in `goshisen v0.1.0` (không phải `dev`). Được bảo vệ trong CI (bước `Verify version stamp`; `internal/cli.Version` phải là `var`, không phải `const` — ldflags `-X` không đặt được hằng số).
- [ ] Đóng gói: `make package VERSION=$TAG` build `goshisen-linux-amd64.tar.gz` + `goshisen-linux-arm64.tar.gz` + `checksums.txt` + `sbom.spdx.json`; `make verify-package VERSION=$TAG` xác thực arch, liên kết tĩnh, đóng dấu phiên bản, checksums, SBOM; các kho lưu trữ tái lập byte. Được bảo vệ bởi job `package` CI và `tests/package`.
- [ ] Workflow phát hành (`.github/workflows/release.yml`) chạy trên thẻ `v*`, build + xác minh, và gắn artifact vào release của thẻ dưới dạng **draft** (không bao giờ tự công bố).
- [ ] Công bố image container (tùy chọn) từ checkout đã gắn thẻ với `--cap-drop=ALL --cap-add=NET_ADMIN --cap-add=NET_RAW` theo hướng dẫn Dockerfile.

## 6. Ghi chú phát hành (draft)

- Tóm tắt nền tảng; củng cố: giám sát tràn hàng đợi, củng cố crash/restart + sẵn sàng, ví dụ supervisor, căn chỉnh toolchain, củng cố bảo mật (enum metric có kiểu, vệ sinh cấu hình mẫu).
- Ghi chú tường minh: bộ đặc quyền phải PASS trước khi tuyên bố sẵn sàng production; §2 của checklist này là gate.

---

## Gate phát hành OpenWrt (bổ sung)

- [x] Pin gói OpenWrt: `PKG_SOURCE_VERSION` trỏ tới commit **đã push** và `PKG_MIRROR_HASH` được đặt (xác minh với `curl -fsSL https://codeload.github.com/.../tar.gz/<sha>` + `sha256sum`).
- [x] `make build` tạo `bin/goshisen` **liên kết tĩnh** (`make verify-static` PASS); job build CI khẳng định qua `file` và `ldd`.
- [x] Build SDK OpenWrt của `package/feeds/goshisen` thành công chỉ từ git pin (không override file://), và payload `.apk` chứa: script init (với cổng `service_enabled`), UCI (netdev `interface`), chính sách fail-closed, nhị phân tĩnh, `prerm` chạy `hook cleanup`.
- [x] Vòng đời procd VM OpenWrt: start/stop/restart/reload (SIGHUP nguyên tử; chính sách không hợp lệ giữ cái trước + ghi log), tắt-mặc định, bền vững boot (daemon tự khởi động, queues 100/101 bound), respawn sau SIGKILL với lưu lượng bị chặn trong khoảng trống và không có bảng trùng, `uci commit` không gây vòng lặp restart.
- [x] apk install/upgrade/remove: không cửa sổ fail-open; `apk del` gỡ bảng `inet goshisen` do ứng dụng sở hữu (không có quy tắc queue mồ côi); cấu hình người dùng sống sót như conffiles.
- [x] Gate phát hành: **dry-run bản build gói từ đúng commit trước khi gắn thẻ** — chạy `make package/feeds/goshisen/{download,compile}` trong container SDK chính thức sạch tại đúng commit và ghi apk sha256 + sha256 nhị phân daemon vào /data/ kèm SHA commit (mirror hash phải là giá trị SDK chính thức sạch, vd: 1867a2de... cho 0.2.3). Trạng thái ký APK: hồ sơ duyệt đã điền, `GH_RELEASE_APK_SIGNING_KEY` đã cấp (chỉ kiểm tra hiện diện), anchor đã merge (PR #34, vân tay `542bb7ae...` đã xác minh). Gate production (GitHub Actions, container SDK) PASS; cài/nâng cấp/gỡ trên VM sạch không cần `--allow-untrusted` PASS; chưa ký/sai khóa bị từ chối; gate `--sha256` từ chối lệch checksum/provenance; khóa CLI/anchor bị từ chối cho APK. Verdict hiện BLOCKED: chưa có hồ sơ duyệt, `GH_RELEASE_APK_SIGNING_KEY` chưa cấp, anchor APK chưa có; `docs/release/apk-signing-key-custody.md` — mẫu hồ sơ duyệt, quy trình cấp phát (chỉ nhân sự được uỷ quyền), checklist xác minh staging, quy trình luân chuyển/thu hồi/phục hồi. production-gate fail-closed đã xác minh tại `94bd7df` (thiếu secret → BLOCKED exit 1); staging runbook đã tài liệu (cấp secret → commit anchor+vân tay → gate → cài /etc/apk/keys/ → cài trên VM sạch không cần `--allow-untrusted` → negative controls dạng production). Verdict vẫn BLOCKED: `GH_RELEASE_APK_SIGNING_KEY` chưa cấp, anchor APK chưa có, hồ sơ chủ sở hữu/luân chuyển/hồi lùi chưa ghi. `scripts/apk-release-gate.sh` (ký gated bằng secret → `apk-verify.sh --sha256` → manifest provenance; mã thoát fail-closed) + `.github/workflows/apk-sign.yml` (chế độ test đầu-cuối; chế độ production yêu cầu `GH_RELEASE_APK_SIGNING_KEY` + anchor/vân tay đã duyệt; job negative-controls). Ký APK production vẫn BLOCKED cho đến khi cấp khóa/anchor được duyệt; không bao giờ `--allow-untrusted` trên đường production; không công bố feed/release từ workflow này. `scripts/apk-sign.sh` ký apk v3 bằng khóa tạm thời; `scripts/apk-verify.sh` xác minh chống thư mục tin cậy với `--sha256` tùy chọn; đã xác minh trên VM OpenWrt sạch: `apk add <signed>` thành công KHÔNG cần `--allow-untrusted` (pubkey trong `/etc/apk/keys/`); apk chưa ký và bị sửa bị trình quản lý gói từ chối.
- [x] IPv6: hook `inet` xếp hàng cả hai họ; allow/deny IPv6 không trạng thái đã xác minh trên VM; v6 có trạng thái đã xác minh trên kernel 6.12.112 với mẫu quy tắc `ESTABLISHED/RELATED` hai chiều chuẩn (issue #5; `docs/release/issue-5-investigation.md`; job CI e2e v6).
- [x] Flow offloading: daemon WARN + metric `goshisen_flow_offload_state` (0 tắt/1 bật/2 không biết), kiểm tra lại mỗi 10 s; `offload_policy` (warn|refuse|readiness|enforce) cấu hình/UCI được kết nối kèm test; `enforce` tắt và xác minh (fail-closed nếu tắt lỗi hoặc trạng thái không biết), cửa sổ chẩn đoán `allow_bypass`; CLI `offload status/disable`; khuyến nghị UCI đã tài liệu hoá; bằng chứng lab OpenWrt sống (`docs/release/offload-lab-evidence.md`).
- [x] Gói sai định dạng: đường lỗi parse sống đã xác minh trên VM (`goshisen_backend_errors_total{kind="parse"}`), fuzz ≥ 60 s sạch.
- [x] Đường phục hồi đã tài liệu và test: `goshisen hook cleanup` khôi phục forward sau stop.

- [ ] **Chữ ký thẻ phát hành (v0.2.5+, docs/release/signed-tags-plan.md):** sau khi ký thẻ, xác minh `git tag -v v0.2.5` PASS với anchor tin cậy đã commit (`docs/trust/allowed_signers` hoặc khóa GPG) và ghi đầu ra xác minh thô + vân tay vào tài liệu bằng chứng phát hành.
- [ ] **Gate khớp pin/phiên bản (v0.2.4+, issue #49):** sau khi gắn thẻ, commit follow-up chỉ-sửa-Makefile (pin = SHA commit thẻ, `PKG_VERSION` = phiên bản thẻ) và chạy `scripts/release-pin-gate.sh --apk goshisen-<ver>-r<rel>.apk --tag v<ver> --payload <extracted binary>`; nó phải PASS (PKG_VERSION == phiên bản thẻ, pin là tổ tiên của thẻ, diff runtime rỗng trên cmd/internal/pkg/go.mod/go.sum/openwrt/files, payload `--version` == PKG_VERSION, tên tệp apk == PKG_VERSION-r<PKG_RELEASE>). Xem `docs/release/release-pin-gate.md` để biết thiết kế và quy trình.

## Xác minh sau phát hành (v0.2.3+)

Chạy sau mỗi lần công bố; mọi mục phải PASS trước khi phát hành được tuyên bố hoàn tất.

- [ ] Xác minh lại công khai (không xác thực): `curl -fsSL https://api.github.com/repos/88kbleirad/GoShiSen-Enforce/releases/tags/vX` → `draft=false`, `prerelease=false`, số lượng asset dự kiến; tải từng `browser_download_url` công khai; `sha256sum -c checksums.txt`; xác minh apk với anchor của nó (`keys/goshisen-apk.pub.pem`, vân tay `542bb7ae…`); xác minh chữ ký CLI bằng khóa phát hành (`scripts/verify-signatures.sh --require-sign`).
- [ ] Test VM nâng cấp/negative: từ apk chưa ký đã công bố trước với anchor đã cài và probe liên tục (không fail-open), sau đó các negative — gỡ anchor / chưa ký / bị sửa đều phải bị từ chối (không bao giờ `--allow-untrusted` trên đường production).
- [ ] Quét trạng thái issue: đóng các issue theo dõi đã giải quyết (vd: tracker CI-flake) kèm tham chiếu commit/run sửa; chỉ để mở các mục đang chờ quyết định.
- [ ] Phát hành bất biến: xác nhận `GET /repos/88kbleirad/GoShiSen-Enforce/immutable-releases` → `enabled=true` (cài đặt bật 2026-11-15); không ghi đè/xoá asset sau công bố. Giữ upload `--clobber` của workflow chỉ ở draft trước-công-bố — sau khi công bố, upload `--clobber` fail dưới tính bất biến, đó là lá chắn mong muốn.
- [ ] Chính sách CI flake: nếu một run main sau phát hành fail trên commit chỉ-tài-liệu, chứng minh flake trước khi đụng vào mã (`git diff --name-only <tag-commit> main` phải chỉ-tài-liệu; so sánh cùng job e2e trên các run với SHA giống hệt) rồi rerun job đã fail (`gh api -X POST …/actions/runs/<id>/rerun-failed-jobs`) hoặc áp dụng bản vá chờ-sẵn-sàng đã tài liệu; ghi kết quả vào issue.
