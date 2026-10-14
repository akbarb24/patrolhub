# PRD —PatrolHub (MVP Aplikasi Pencatatan Ronda)

Oct 6, 2026 · @Akbar Binaji

## Ringkasan & latar belakang

PatrolHub menggantikan rekap absen ronda di Excel dengan laporan malam via QR, rekap bulanan publik, dan hitung denda otomatis untuk satu RT.

Saat ini absen dicatat manual di spreadsheet: baris per anggota dikelompokkan per hari jaga (Senin–Minggu), kolom per tanggal, kode `v` hadir, `h` hujan, `1` tidak hadir. Denda dihitung per ketidakhadiran (dari contoh: Rp 5.000 × jumlah `1`) lalu dijumlah per bulan. Pencatat harus mengumpulkan info dari tim tiap malam, mengetik ulang, dan menghitung denda sendiri.

**Tujuan MVP**

- Absen dicatat langsung di pos ronda oleh salah satu anggota tim, malam itu juga.
- Ada bukti kondisi lingkungan: catatan situasi + beberapa foto per malam.
- Warga bisa melihat rekap kehadiran dan denda tiap bulan tanpa login.
- Denda dihitung otomatis dan konsisten dengan aturan RT.

**Metrik sukses (3 bulan pertama)**

| Metrik | Target |
| --- | --- |
| Malam jaga yang punya laporan | ≥ 90% |
| Waktu menyusun rekap bulanan | < 5 menit (dari ± 1 jam manual) |
| Koreksi denda yang diajukan warga | ≤ 2 per bulan |

## Lingkup MVP

MVP mencakup satu RT, satu pos ronda, satu tautan/QR laporan, dan satu atau beberapa admin.

**Termasuk**

- Data anggota ronda dan jadwal jaga mingguan per hari, plus pengganti/tukar jadwal per tanggal.
- Halaman laporan malam via QR/tautan, tanpa login, hanya terbuka 22.00–06.00 (bisa diubah admin).
- Absen tim malam itu, catatan situasi, dan beberapa foto dari kamera langsung.
- Penutupan malam otomatis pukul 06.00 dan perhitungan denda.
- Penandaan malam “hujan” oleh admin (tanpa denda).
- Rekap bulanan publik: ringkasan + kartu per hari jaga, dan linimasa situasi, plus total denda per anggota dan total RT.
- Panel admin: anggota, jadwal, koreksi absen, status bayar denda, pengaturan, cetak QR.
- Teks rekap mingguan/bulanan yang bisa disalin ke grup WhatsApp.

**Di luar lingkup MVP**

- Multi-RT / multi-tenant.
- Login anggota dan aplikasi mobile native.
- Notifikasi otomatis (WhatsApp/push) dan pengingat jadwal.
- Pembayaran denda online.
- Laporan insiden terpisah (pencurian, kebakaran) dan eskalasi ke ketua RT.

## Persona & peran

Ada tiga peran; hanya admin yang login.

| Peran | Siapa | Akses | Kebutuhan utama |
| --- | --- | --- | --- |
| Pelapor ronda | Salah satu anggota tim jaga malam itu | Halaman laporan malam via QR/tautan, tanpa login | Absen cepat dari HP di pos, sambil memotret situasi |
| Warga | Seluruh warga RT, termasuk anggota | Rekap bulanan publik, read-only | Melihat siapa hadir dan berapa denda bulan ini |
| Admin | Ketua RT / seksi keamanan / bendahara | Panel admin, login | Mengatur jadwal, menandai hujan, mengoreksi absen, mencatat denda lunas |

Catatan: di spreadsheet ada baris seperti “C-10”, “E-11”, dan “Pak Eres”. Karena itu anggota dimodelkan sebagai *peserta ronda* yang punya blok rumah (wajib) dan nama (opsional); tampil sebagai “Nama (Blok)”, atau blok saja bila nama kosong.

## Kebutuhan fungsional

Prioritas: **Must** wajib untuk rilis MVP, **Should** dikerjakan bila waktu cukup.

### F1 — Anggota & jadwal

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| F1.1 | Admin menambah, mengubah, menonaktifkan peserta ronda: blok rumah (wajib, mis. C-10), nama (opsional), no. HP (opsional), catatan. Tampil sebagai “Nama (Blok)”, atau blok saja bila nama kosong. Peserta nonaktif tetap muncul di rekap lama. | Must |
| F1.2 | Admin menetapkan hari jaga tetap per peserta (Senin–Minggu), berlaku mulai tanggal tertentu. Satu peserta boleh punya lebih dari satu hari. | Must |
| F1.3 | Admin membuat perubahan per tanggal: tambah peserta, hapus peserta, atau tukar (A diganti B) untuk satu malam. | Must |
| F1.4 | Tim malam = jadwal tetap hari itu + perubahan per tanggal. Dihitung saat halaman dibuka, jadi selalu dinamis. | Must |
| F1.5 | Pratinjau jadwal satu bulan untuk admin sebelum bulan berjalan. | Should |

### F2 — Halaman laporan malam (QR)

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| F2.1 | Satu tautan tetap (dicetak sebagai QR di pos). Tanpa login. | Must |
| F2.2 | Hanya terbuka pukul 22.00 sampai 06.00 keesokan harinya (WIB, bisa diubah admin). Di luar jam itu tampil pesan “Laporan dibuka pukul 22.00” beserta tim malam ini. | Must |
| F2.3 | Laporan setelah tengah malam tetap milik *malam sebelumnya* (contoh: 02.00 tanggal 8 = malam 7 Oktober). | Must |
| F2.4 | Menampilkan judul “Laporan Ronda RT02”, tanggal malam, dan daftar tim malam itu. | Must |
| F2.5 | Tiap anggota punya toggle Hadir / Tidak hadir (bawaan: belum dicentang = tidak hadir). Disimpan langsung tiap kali diubah. | Must |
| F2.6 | Kolom teks “Situasi” (maks. 1.000 karakter), bisa diubah sepanjang malam. | Must |
| F2.7 | Tombol “Ambil Foto” membuka kamera belakang langsung (bukan galeri). Bisa berkali-kali; tiap foto disimpan dengan jam ambil dan tampil sebagai thumbnail. | Must |
| F2.8 | Minimal 3 foto (bisa diubah admin) sebelum laporan dianggap sah. Maks. 10 foto per malam, dikompres di sisi klien (± 1280 px, < 500 KB). | Must |
| F2.9 | Lokasi GPS dicatat bila diizinkan, tapi tidak memblokir laporan. | Should |
| F2.10 | Anggota bisa ditambah dari daftar peserta bila ada yang ikut jaga di luar jadwal (dicatat sebagai “pengganti”). | Should |
| F2.11 | Siapa pun yang memegang tautan dapat mengedit sampai 06.00; setiap perubahan tercatat (waktu, IP, user agent). | Must |

### F3 — Penutupan malam & denda

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| F3.1 | Pukul 06.00 job otomatis menutup malam: status absen dibekukan dan denda dihitung. | Must |
| F3.2 | Jika tidak ada laporan sama sekali, malam ditandai “Tidak ada laporan” (aturan denda di bagian Aturan bisnis). | Must |
| F3.3 | Pelapor dapat menekan “Ajukan Hujan” di halaman laporan, lalu admin mengonfirmasi atau menolak; admin juga bisa langsung menandai malam sebagai “Hujan” (sebelum atau sesudah ditutup); semua anggota malam itu berstatus `h`, tanpa denda. | Must |
| F3.4 | Nominal denda per ketidakhadiran diatur admin (bawaan Rp 5.000) dan disimpan per absen saat malam ditutup, sehingga perubahan tarif tidak mengubah bulan lalu. | Must |
| F3.5 | Admin mencatat pelunasan denda per peserta per bulan (tanggal bayar, nominal). | Should |

### F4 — Rekap bulanan publik

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| F4.1 | Halaman publik berjudul “Rekap Ronda \[Bulan Tahun\]” (mis. Rekap Ronda September 2026) dengan pemilih bulan dan dua tab: Ringkasan dan Situasi. | Must |
| F4.2 | Ringkasan bulan di bagian atas: malam terlapor (x/y, plus jumlah malam hujan dan tanpa laporan), persentase kehadiran, total denda bulan itu, dan jumlah peserta yang kena denda. | Must |
| F4.3 | Kartu per hari jaga (Senin–Minggu): tiap baris peserta “Nama (Blok)”, satu penanda per malam jaga dengan tanggal di atasnya, denda per peserta, dan subtotal tim. | Must |
| F4.4 | Penanda status dibedakan dengan warna dan ikon (centang = hadir, silang = tidak hadir, “h” = hujan), disertai legenda. Malam yang belum terjadi tidak ditampilkan. | Must |
| F4.5 | Tab Situasi: linimasa per malam, terbaru di atas, berisi tanggal, hari, jumlah hadir (atau label Hujan / Tanpa laporan), dan catatan situasi. Foto tidak tampil publik. | Must |
| F4.6 | Filter “Ada catatan khusus” di linimasa untuk malam yang catatannya bukan sekadar “Aman”. | Should |
| F4.7 | Tombol “Salin rekap” menghasilkan teks ringkas untuk grup WhatsApp (mingguan dan bulanan). | Should |

### F5 — Panel admin

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| F5.1 | Login username + password; beberapa akun admin. | Must |
| F5.2 | Daftar malam: status, pelapor terakhir, foto, situasi; admin bisa mengoreksi absen dengan alasan wajib. | Must |
| F5.3 | Pengaturan: nama RT, jam buka/tutup, nominal denda, minimal foto. | Must |
| F5.4 | Halaman cetak QR (A5) berisi tautan laporan. | Must |
| F5.5 | Ekspor rekap bulanan ke Excel/CSV. | Should |

## Alur pengguna utama

**A. Laporan malam (pelapor ronda)**

1. Sekitar 22.00 salah satu anggota tim memindai QR di pos.
2. Halaman menampilkan “Laporan Ronda RT02 — 07 Oktober 2026” dan tim malam itu (mis. Anton, Joshua, Robert).
3. Pelapor mencentang anggota yang hadir; tiap perubahan langsung tersimpan.
4. Pelapor menulis situasi (mis. “Aman”) dan menekan “Ambil Foto”; foto diunggah dan muncul sebagai thumbnail.
5. Sepanjang malam, anggota mana pun bisa membuka tautan lagi untuk menambah foto, memperbarui situasi, atau mencentang anggota yang datang terlambat.
6. Pukul 06.00 halaman tertutup; malam dibekukan dan denda dihitung.

**B. Malam hujan (admin)**

1. Admin membuka daftar malam, memilih tanggal, menekan “Tandai hujan”.
2. Semua anggota malam itu menjadi `h` dan denda malam itu dihapus.

**C. Warga melihat rekap**

1. Warga membuka tautan rekap (mis. dari grup WhatsApp).
2. Grid bulan berjalan tampil; warga bisa pindah ke bulan lain dan melihat total denda tiap peserta.

**D. Admin menyiapkan jadwal**

1. Admin menambah peserta dan menetapkan hari jaga tetap.
2. Untuk tanggal tertentu, admin menambahkan tukar/pengganti.
3. Admin mencetak QR dan menempelkannya di pos.

## Aturan bisnis

Satu “malam ronda” = tanggal mulai jaga; jendela laporannya dari jam buka tanggal itu sampai jam tutup keesokan harinya, zona Asia/Jakarta.

| Kondisi saat malam ditutup (06.00) | Status malam | Status anggota | Denda |
| --- | --- | --- | --- |
| Laporan ada, ≥ 3 foto | Dilaporkan | Dicentang = `v`, tidak dicentang = `1` | Rp 5.000 per `1` |
| Laporan ada, < 3 foto | Dilaporkan (bukti kurang) | Seperti di atas, diberi tanda untuk ditinjau admin | Seperti di atas |
| Tidak ada laporan sama sekali | Tidak ada laporan | Semua `1` | Rp 5.000 per anggota |
| Ditandai hujan oleh admin | Hujan | Semua `h` | Tidak ada |

Aturan tambahan:

- Denda bulanan peserta = jumlah denda semua malamnya pada bulan kalender itu.
- Koreksi admin setelah malam ditutup menghitung ulang denda malam itu dan menyimpan jejak (siapa, kapan, alasan).
- Peserta yang ditambahkan sebagai pengganti (F1.3/F2.10) diperlakukan sama seperti anggota terjadwal.
- Jadwal dibaca dari aturan yang berlaku pada tanggal malam itu; perubahan jadwal tidak mengubah malam yang sudah ditutup.

* Jika pelapor mengajukan hujan, malam berstatus “Menunggu konfirmasi hujan”; denda baru dihitung setelah admin mengonfirmasi (jadi `h`) atau menolak (dihitung seperti biasa).
* Rekap dan denda memakai bulan kalender yang sama: Rekap September berisi absen dan denda September.
* Tidak ada status “izin” di MVP; tidak hadir tanpa hujan selalu didenda.

## Model data

Delapan tabel PostgreSQL; nama di kode memakai bahasa Inggris (“ronda” = `patrol`).

| Tabel | Kolom utama | Keterangan |
| --- | --- | --- |
| `member` | id, house\_block, name, phone, notes, active, created\_at | Peserta ronda; tampil “Nama (Blok)” atau blok saja |
| `schedule_rule` | id, member\_id, day\_of\_week, effective\_from, effective\_to | Hari jaga tetap mingguan |
| `schedule_override` | id, patrol\_date, action (ADD / REMOVE / SWAP), member\_id, replacement\_member\_id, note | Perubahan jadwal satu malam |
| `patrol_night` | id, patrol\_date (unik), status (OPEN / REPORTED / NO\_REPORT / RAIN\_REQUESTED / RAINED\_OUT), situation\_note, first\_reported\_at, closed\_at, needs\_review | Satu baris per malam, dibuat saat halaman pertama dibuka atau saat penutupan |
| `patrol_attendance` | id, night\_id, member\_id, status (PRESENT / ABSENT / RAINED\_OUT), source (SCHEDULED / OVERRIDE / WALK\_IN), fine\_amount, updated\_at | Absen per anggota; fine\_amount diisi saat malam ditutup |
| `patrol_photo` | id, night\_id, storage\_key, taken\_at, latitude, longitude, size\_bytes | Foto malam itu |
| `audit_log` | id, entity, entity\_id, action, actor (ANONYMOUS / admin id), ip, user\_agent, reason, created\_at | Jejak semua perubahan laporan dan koreksi |
| `fine_payment` | id, member\_id, period (YYYY-MM), amount, paid\_at, recorded\_by | Pelunasan denda (Should) |

Pengaturan (`setting`: key, value) dan akun admin (`admin_user`) disimpan terpisah. Kunci pengaturan: `rt_name`, `window_open`, `window_close`, `fine_per_absence`, `min_photos`, `max_photos`.

## Halaman & route

Route berbahasa Inggris, teks UI berbahasa Indonesia; interaksi kecil memakai HTMX.

| Halaman | Route | Akses |
| --- | --- | --- |
| Laporan malam ini (target QR) | `GET /patrol/tonight` | Publik, hanya dalam jendela waktu |
| Toggle hadir | `POST /patrol/tonight/attendance/{memberId}` | Publik, dalam jendela |
| Simpan situasi | `POST /patrol/tonight/situation` | Publik, dalam jendela |
| Unggah foto | `POST /patrol/tonight/photos` | Publik, dalam jendela |
| Rekap bulanan | `GET /recap/{yyyy}/{mm}` (`/recap` → bulan berjalan) | Publik |
| Linimasa situasi | `GET /recap/{yyyy}/{mm}/situations` | Publik |
| Login admin | `GET/POST /admin/login` | Publik |
| Anggota | `/admin/members` | Admin |
| Jadwal & perubahan | `/admin/schedule`, `/admin/schedule/overrides` | Admin |
| Daftar malam, koreksi, tandai hujan | `/admin/nights`, `/admin/nights/{date}` | Admin |
| Denda & pelunasan | `/admin/fines/{yyyy}/{mm}` | Admin |
| Pengaturan & cetak QR | `/admin/settings`, `/admin/qr` | Admin |

Tampilan halaman laporan mengikuti sketsa: judul RT, tanggal malam, daftar “Tim Ronda” dengan toggle per nama, kotak “Situasi”, tombol “Ambil Foto”, lalu strip thumbnail foto yang sudah diambil.

## Kebutuhan non-fungsional & stack teknis

Stack: Go (router `net/http` bawaan, Chi untuk middleware), templ untuk template server-side, HTMX + Alpine.js, TailwindCSS + daisyUI, PostgreSQL via pgx + sqlc, migrasi dengan goose. Layer handler/service/repository dalam modul `patrolhub` dengan struktur `cmd/` dan `internal/`.

| Aspek | Kebutuhan |
| --- | --- |
| Perangkat | Mobile-first; halaman laporan berjalan baik di Android kelas bawah dan jaringan 3G (halaman < 200 KB tanpa foto). |
| Waktu | Semua logika jendela dan penutupan memakai `Asia/Jakarta`; job penutupan berjalan tiap 5 menit di goroutine terjadwal dan idempoten; data zona waktu disertakan di binary (import time/tzdata). |
| Foto | Kamera langsung via `<input type="file" accept="image/*" capture="environment">`, dikompres di klien; disimpan di disk lokal atau S3-compatible (mis. MinIO); retensi bawaan 6 bulan. |
| Keamanan halaman publik | Tautan tetap tanpa login; rate limit per IP (mis. 60 req/menit), validasi tipe/ukuran file, semua perubahan masuk `audit_log`. |
| Keamanan admin | Password di-hash (bcrypt/argon2), sesi cookie HttpOnly, CSRF token pada form. |
| Privasi | Rekap publik hanya menampilkan nama/blok, status, denda, dan catatan situasi; foto dan no. HP tidak pernah tampil publik. |
| Ketersediaan | Satu instance cukup; backup harian database + folder foto. |
| Deploy | Satu binary Go dalam image Docker minimal (distroless) + Postgres, bisa di VPS kecil atau homelab. |

## Pertanyaan terbuka & usulan fitur berikutnya

**Keputusan yang sudah diambil**

- [x] Rekap bulanan berisi absen dan denda bulan yang sama (Rekap September = denda September); soal penagihan tidak ditampilkan.
- [x] Malam tanpa laporan sama sekali: semua anggota otomatis didenda.
- [x] Rekap publik menampilkan catatan situasi; foto hanya untuk admin.
- [x] Pelapor boleh mengajukan “hujan”, dikonfirmasi admin.
- [x] Status “izin” belum diperlukan.
- [x] “C-10”, “E-11” dll. adalah blok rumah; tampil “Nama (Blok)”, atau blok saja bila tanpa nama.
- [x] Minimal 3 foto per malam.

&#32;Iya tetntu saja ditagih di bulan berikutnya

&#32;saat ini otomatis didenda

&#32;Cukup situasi, foto tidak perlu

&#32;Boleh

&#32;Saat ini tidak perlu

&#32;Ya itu adalah nama blok Rumah, sebaiknya dicantunmkan Nama dan Blok Rumah apabila tidak ada nama maka muncul nama Blok Rumah

&#32;Minimal 3 foto

**Usulan fase 2**

- Notifikasi WhatsApp otomatis: pengingat jadwal sore hari dan rekap mingguan ke grup.
- QR atau token yang berganti tiap malam untuk mencegah laporan dari luar pos.
- Laporan kejadian (insiden) terpisah dengan eskalasi ke ketua RT.
- Tampilan rekap tambahan: kalender bulanan, “Cek jadwal & denda saya” (cari nama/blok), dan tabel gaya Excel untuk desktop/cetak.
- Dashboard statistik: tingkat kehadiran per peserta, malam tanpa laporan per bulan.
- Pembayaran denda via QRIS dan rekap kas ronda.
