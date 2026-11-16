# InBlack Top-Up Platform

ระบบแพลตฟอร์มเติมเกมและซื้อแพ็กเกจดิจิทัลแบบครบวงจร ออกแบบมาเพื่อรองรับการทำธุรกรรมพร้อมกันจำนวนมาก (High Concurrency) ป้องกันการขายเกินสต็อก (Oversell) การเติมเกมซ้ำซ้อน และการคืนเงินเกินจำนวน ภายใต้แนวคิด **"ไม่เสียเงินและไม่เสียของ"**

---

## 1. สถาปัตยกรรมและเทคโนโลยีหลัก (Tech Stack)

* **Frontend (`web`):** Next.js (App Router), TypeScript (`strict: true`), Tailwind CSS, Server-Sent Events (SSE)
* **Backend API (`api-1`, `api-2`):** Go (Golang) รันแบบ Horizontal Scale-out 2 Replicas ผ่าน Reverse Proxy
* **Background Worker (`worker`):** Go Worker แยก Process สำหรับประมวลผล Queue, Fulfillment, Retry, Order Expiration และ Reconciliation
* **Reverse Proxy (`proxy`):** Nginx (Load Balancing แบบ Round-Robin ไปยัง `api-1` และ `api-2`)
* **Database (`postgres`):** PostgreSQL 16 (ใช้ Row-level Locking `FOR UPDATE`, Serializable/Repeatable Read และ Database Constraints)
* **Cache / Queue / Pub-Sub (`redis`):** Redis 7 (Idempotency Store, Rate Limiting, Redis Streams สำหรับ Job Queue และ Redis Pub/Sub สำหรับกระจาย Event ข้าม API Replica)
* **Mock Services (`mock-pay`, `mock-game`):** บริการจำลอง Payment Gateway และระบบเติมเกมปลายทางพร้อมตัวจำลองความล้มเหลว (Latency, Timeout, 5xx)
* **Email Testing (`mailpit`):** ระบบรับอีเมลจำลองสำหรับทดสอบ OTP และใบเสร็จรับเงิน

---

## 2. วิธีการรันระบบ (Quick Start)

ระบบถูกออกแบบให้สามารถรันทั้งหมดได้ด้วยคำสั่งเดียวโดยไม่ต้องตั้งค่าเพิ่มเติม:

```bash
# 1. คัดลอกไฟล์ตั้งค่าตัวอย่าง (หากยังไม่มี .env)
cp .env.example .env

# 2. สร้างและรัน Container ทั้งหมดพร้อม Seed Data อัตโนมัติ
docker compose up --build -d
```

### ช่องทางเข้าใช้งาน (Endpoints)
* **หน้าเว็บไซต์หลัก (ลูกค้า):** `http://localhost:8080` (หรือ `http://localhost:3000`)
* **ระบบหลังบ้าน (Admin Dashboard):** `http://localhost:8080/admin/login`
* **หน้าจำลองการชำระเงิน (Mock Pay UI):** เปิดอัตโนมัติเมื่อสร้างออเดอร์ หรือเข้าผ่าน `http://localhost:8080/mock-pay`
* **กล่องข้อความอีเมล (Mailpit UI):** `http://localhost:8025`
* **API Health & Metrics:** `http://localhost:8080/healthz`, `http://localhost:8080/readyz`, `http://localhost:8080/metrics`

---

## 3. ข้อมูลบัญชีทดสอบ (Seeded Test Credentials)

ระบบได้ทำการ Seed บัญชีผู้ดูแลระบบ (Admin) ครบทั้ง 4 ระดับสิทธิ์ (RBAC) ตามข้อกำหนด:

| Role (บทบาท) | Email (อีเมล) | Password (รหัสผ่าน) | สิทธิ์การใช้งาน |
| :--- | :--- | :--- | :--- |
| **Superadmin** | `superadmin@inblackm.com` | `SuperAdmin@2026!` | สิทธิ์สูงสุด: จัดการสินค้า, โค้ดส่วนลด, ผู้ใช้ Admin และสิทธิ์ทั้งหมด |
| **Finance** | `finance@inblackm.com` | `Finance@2026!` | ดู PII เต็ม, สั่ง Retry, ทำรายการ Refund (เต็มจำนวน/บางส่วน), รัน Reconciliation |
| **Support** | `support@inblackm.com` | `Support@2026!` | ดู PII เต็ม (ไม่ Mask อีเมล/Game UID), สั่ง Retry Fulfillment |
| **Viewer** | `viewer@inblackm.com` | `Viewer@2026!` | ดูรายการออเดอร์และ Dashboard (ข้อมูล PII จะถูก Mask ทั้งหมด) |

> **หมายเหตุสำหรับลูกค้าทั่วไป:** สามารถเข้าสู่ระบบหน้า "ออเดอร์ของฉัน" ด้วยอีเมลใดก็ได้ ระบบจะส่งรหัส **OTP 6 หลัก** ไปที่ Mailpit (`http://localhost:8025`)

---

## 4. ข้อมูลสินค้าและโค้ดส่วนลดเริ่มต้น (Seed Data)

* **สินค้า:**
  * `DIAMOND-1000` (1,000 Diamonds) - 349.00 บาท (`34,900` สตางค์) - Stock ไม่จำกัด
  * `DIAMOND-5000` (5,000 Diamonds) - 1,590.00 บาท (`159,000` สตางค์) - Stock ไม่จำกัด
  * `MONTHLY-PASS` (Monthly Pass) - 179.00 บาท (`17,900` สตางค์) - Stock ไม่จำกัด, จำกัด 1 ชิ้น/ออเดอร์
  * `FLASH-SKIN-01` (Limited Skin) - 99.00 บาท (`9,900` สตางค์) - **Stock 100 ชิ้น**, จำกัด 2 ชิ้น/อีเมล
* **โค้ดส่วนลด:**
  * `SAVE10`: ลด 10% สูงสุด 50.00 บาท (`5,000` สตางค์), ขั้นต่ำ 200.00 บาท (`20,000` สตางค์), จำกัด 10 สิทธิ์ทั้งระบบ และ 1 สิทธิ์/อีเมล
  * `FLAT20`: ลด 20.00 บาท (`2,000` สตางค์), ขั้นต่ำ 100.00 บาท (`10,000` สตางค์), ไม่จำกัดจำนวน

---

## 5. การรันชุดทดสอบอัตโนมัติ (Testing)

```bash
# รัน Unit Tests, Integration Tests และ Invariant Concurrency Tests ทั้งหมด
make test

# รัน Playwright End-to-End Tests
cd web && npx playwright test
```

---

## 6. ขอบเขตที่ทำเสร็จและข้อจำกัด (Status & Scope)

### ส่วนที่พัฒนาเสร็จสมบูรณ์ (Completed)
- [x] **Core Domain & Financial Math:** คำนวณราคา, ค่าธรรมเนียม, ส่วนลด และ VAT เป็นจำนวนเต็มหน่วยสตางค์ 100% พร้อมตรวจสอบ Checksum เลขออเดอร์ `mod 997` ทั้งฝั่ง Client และ Server
- [x] **Concurrency & Invariants:** ป้องกัน Oversell สินค้า Flash Sale, ป้องกันการใช้โค้ดเกินสิทธิ์, ป้องกันการเติมเกมซ้ำ และป้องกันการคืนเงินเกินยอดจ่าย
- [x] **State Machine & Audit Trail:** บังคับ State Transition ระดับ Domain/DB พร้อมบันทึก `order_events` แบบ Append-only
- [x] **Payment & Webhook Reliability:** ตรวจสอบ HMAC-SHA256 จาก Raw Body, Replay Protection (`300s`), Idempotency และจัดการ Webhook มาช้า/สลับลำดับ/ยอดไม่ตรง
- [x] **Worker & Resilience:** ระบบ Queue พร้อม Exponential Backoff + Jitter, Dead-Letter Queue (DLQ) และ Lease Recovery สำหรับงานค้างเกิน 2 นาที
- [x] **Real-time SSE Scale-out:** กระจายสถานะข้าม `api-1` และ `api-2` ผ่าน Redis Pub/Sub พร้อมรองรับ `Last-Event-ID`
- [x] **Security & RBAC:** Masking PII, Rate Limiting, IDOR Protection (คืนค่า `404`), CSRF Protection, Security Headers (CSP, HSTS)

### ส่วนที่ยังไม่ได้พัฒนา / ข้อจำกัด (Known Limitations)
- [ ] **Mobile Native App (โบนัส):** ยังไม่ได้พัฒนาแอปพลิเคชันแยกบน Flutter/SwiftUI แต่หน้าเว็บถูกออกแบบให้รองรับ Responsive บนหน้าจอขนาดเล็กถึง `320px` อย่างสมบูรณ์
- [ ] **Distributed Tracing UI (Jaeger):** ระบบสร้าง `trace_id` และ `request_id` ใน JSON Log ครบทุก Service แล้ว แต่ยังไม่ได้เพิ่ม Container ของ Jaeger UI ลงใน `docker-compose.yml` เพื่อลดการใช้ทรัพยากรเครื่องขณะทดสอบ
