# Đề xuất Ký Phát Hành (v0.2.1+ / Issue #12)

> **Trạng thái: đã được thay thế.** Phương án C (OpenSSL Ed25519 raw-sign,
> không dùng công cụ ngoài) đã được triển khai; xem `docs/release/signing-design.md`
> để biết thiết kế và kế hoạch di trú. Tệp này được giữ lại để làm hồ sơ lịch
> sử. Quy trình phát hành vẫn đính kèm tài sản chưa ký cho đến khi quá trình
> khởi tạo khóa đã được duyệt (bước di trú 2) hoàn tất.

Không có khóa hay bí mật nào được lưu trong kho lưu trữ này. Hiện chưa
có gì được triển khai; quy trình phát hành vẫn đính kèm tài sản chưa ký
cho đến khi người duy trì chấp thuận một phương án và một chính sách khóa.

## Mục tiêu

- Cho phép người dùng xác minh rằng `.apk` (gói) và các tarball phát hành
  đến từ dự án (toàn vẹn + nguồn gốc) mà không cần `--allow-untrusted`.
- Giữ thay đổi ở mức tối thiểu: không thêm bí mật dài hạn mới trong CI,
  không thay đổi mô hình build, và không bắt buộc người dùng chạy thêm công
  cụ nào trừ khi họ muốn sự bảo đảm mạnh hơn.
- Bảo toàn các đặc tính hiện có: tài sản được xếp tầng dưới dạng bản phát
  hành nháp, không bao giờ tự động xuất bản; PR không bao giờ tạo ra hiện
  vật đã ký.

## Các phương án đã cân nhắc

### A. Cosign keyless (Sigstore) — được khuyến nghị cho tarball + SBOM

- Ký `goshisen-linux-{amd64,arm64}.tar.gz`, `checksums.txt`, và
  `sbom.spdx.json` bằng `cosign sign-blob` dùng danh tính OIDC của
  luồng công việc GitHub Actions (token từ `actions/id-token`).
- Người dùng xác minh bằng `cosign verify-blob --certificate-identity
  "https://github.com/88kbleirad/GoShiSen-Enforce/.github/workflows/release.yml@refs/tags/v0.2.1"
  --certificate-oidc-issuer https://token.actions.githubusercontent.com`.
- Không cần quản lý khóa; độ tin cậy = "bản phát hành được tạo bởi quy
  trình phát hành của kho lưu trữ này trên tag này". Trang phát hành GitHub
  hiển thị từng chữ ký và chứng chỉ.
- Hạn chế: việc xác minh yêu cầu `cosign` (một nhị phân tĩnh đơn lẻ) phía
  người dùng. Mục nhập minh bạch trong Rekor cung cấp dấu vết kiểm toán và
  khả năng xác minh ngoại tuyến qua bundle đã ký.

### B. minisign — được khuyến nghị cho `.apk`

- Tạo một cặp khóa `minisign` **bên ngoài** kho lưu trữ. Khóa bí mật
  phải được giữ trong kho bí mật của người duy trì (hoặc bí mật mã hóa của
  GitHub chỉ được dùng bởi quy trình `release` trên các tag `v*`).
- Khóa công khai (`minisign.pub`) được commit vào kho lưu trữ (ví dụ
  `openwrt/files/etc/apk/keys/goshisen.pub` cho khóa đóng gói, và trong
  ghi chú phát hành cho các tarball).
- Quy trình phát hành ký từng hiện vật (`.apk`, tarball, `checksums.txt`)
  và tải các tệp `.minisig` lên cạnh chúng.
- Người dùng xác minh: `minisign -Vm <file> -P <pubkey>` — không cần tài
  khoản, không cần mạng, hoạt động ngoại tuyến vĩnh viễn.
- Vì sao dùng minisign thay vì một khóa OpenWrt đầy đủ? Việc ký gốc của
  định dạng `.apk` dùng các khóa `usign` (ed25519) được cài dưới
  `/etc/apk/keys/`. Tạo khóa `usign` sẽ cho phép `apk add` xác minh gốc
  mà không cần `--allow-untrusted`; tuy nhiên việc sinh + lưu trữ khóa
  `usign` phức tạp hơn và khóa phải tuân theo quy ước tên tệp
  (`<keyname>-<fingerprint>.pub`). Vì gói cũng được phân phối bên ngoài
  OpenWrt, minisign giữ một công cụ ký duy nhất cho mọi tài sản; `usign`
  gốc apk có thể được thêm lớp sau này mà không phá vỡ thiết kế này.

### Mô hình tin cậy (cả hai phương án)

- Danh tính ký được gắn với tag đã tạo ra chúng: một nhị phân được ký bởi
  quy trình phát hành cho `v0.2.1` không thể bị phát lại như `v0.2.2`.
- Việc lộ token GitHub Actions không làm giả mạo được chữ ký: danh tính
  OIDC ghim đường dẫn quy trình + ref (Cosign), và khóa bí mật minisign
  không có mặt trong CI (chỉ được dùng trong một action được bảo vệ hoặc
  kho bí mật).
- Dự án công bố khóa công khai ngoài kênh (ghi chú phát hành, thư mục
  `/etc/apk/keys/` của gói) để độ tin cậy khi cài đặt lần đầu được neo
  độc lập với kênh phân phối tài sản.
- Chúng tôi **không** khuyến nghị dùng `--allow-untrusted` làm mặc định;
  kiểm tra sha256 vẫn là biện pháp kiểm soát thứ cấp, không thay thế cho
  chữ ký.

## Điều người dùng xác minh

- Tarball: `cosign verify-blob` (sự hiện diện của bundle đã ký trong bản
  phát hành) hoặc `minisign -Vm` (tệp `.minisig`).
- `.apk`: `apk add` không dùng `--allow-untrusted` (khi khóa công khai phát
  hành đã nằm trong `/etc/apk/keys/`), hoặc `minisign -Vm` trên `.apk`.

## Triển khai tối thiểu được đề xuất (khi được duyệt)

1. Thêm một job `sign` vào `.github/workflows/release.yml` chỉ chạy trên
   các tag `v*`, có `permissions: id-token: write, contents: write`, và:
   - cài `cosign` (phiên bản ghim, checksum đã xác minh) và `minisign`;
   - ký `dist/*` bằng `cosign sign-blob` và ghi các tệp
     `*.sig`/`*.cert`/`*.bundle`;
   - ký `dist/*` bằng `minisign` dùng khóa bí mật từ
     `GH_SECRET_MINISIGN_KEY` (một bí mật GitHub Actions, không bao giờ
     được commit);
   - tải toàn bộ hiện vật chữ ký lên bản phát hành nháp.
2. Thêm một bước CI `verify-signatures` cho PR để kiểm tra *hình dạng* của
   job ký (sự hiện diện, các tệp dự kiến) mà không thực hiện việc ký thật
   (không có bí mật trên PR).
3. Ghi lại các lệnh trong `docs/installation.md` và `docs/openwrt.md`
   (thay thế các ví dụ `--allow-untrusted` khi đã có khóa).

## Loại trừ tường minh

- Giao diện quản lý khóa hoặc tự động hóa luân chuyển khóa.
- Ký digest SBOM tách rời khỏi các tarball (SBOM đã nằm bên trong tarball
  và được liệt kê trong `checksums.txt`).
- Mô hình web-of-trust / đánh giá khóa.
