# คู่มือปฏิบัติการฉุกเฉิน (Incident Response Runbook)

เอกสารนี้จัดทำขึ้นสำหรับทีมวิศวกรและทีมปฏิบัติการ (On-Call) เพื่อใช้ตรวจสอบและแก้ไขปัญหาวิกฤตที่อาจเกิดขึ้นในระบบ InBlack Top-Up Platform

---

## กรณีที่ 1: มีงานค้างใน Dead-Letter Queue (DLQ) จำนวนมาก

### อาการและการตรวจพบ (Symptoms)
* หน้า Admin Dashboard แสดงตัวเลข `Dead-Letter Queue > 0`
* Metric `worker_dlq_jobs_total` เพิ่มขึ้นอย่างต่อเนื่อง
* มีออเดอร์จำนวนมากเปลี่ยนสถานะเป็น `NEEDS_REVIEW` หรือ `FAILED`

### ขั้นตอนการตรวจสอบ (Diagnosis)
1. ตรวจสอบ Log ของ `worker` เพื่อหาสาเหตุหลัก (Root Cause) เช่น Payment Gateway หรือ Game Provider ปลายทางขัดข้อง หรือเกิด Bug ในการ Parse ข้อมูล:
   ```bash
   docker compose logs --tail=200 worker | grep "DLQ"
   ```
2. ตรวจสอบรายชื่อออเดอร์ที่ได้รับผลกระทบในฐานข้อมูล:
   ```sql
   SELECT order_id, status, updated_at FROM orders WHERE status = 'NEEDS_REVIEW' ORDER BY updated_at DESC LIMIT 50;
   ```

### ขั้นตอนการแก้ไข (Remediation)
1. **หากเกิดจากระบบภายนอกล่มชั่วคราว (Transient Failure):**
   * รอจนกว่าระบบภายนอกกลับมาปกติ (ตรวจสอบผ่าน `/healthz` หรือหน้าสถานะของผู้ให้บริการ)
   * ใช้สิทธิ์ `Support` หรือ `Finance` เข้าไปที่หน้า Admin -> เลือกออเดอร์ที่มีปัญหา -> กดปุ่ม **"Retry Fulfillment"** (หรือยิง API `POST /api/v1/admin/orders/{id}/retry-fulfillment`)
2. **หากเกิดจากข้อมูลผิดพลาด (Poison Pill):**
   * ตรวจสอบ Payload ใน `order_events` และ `fulfillment_attempts`
   * หากไม่สามารถเติมเกมได้จริง ให้ประสานงานทีม Finance เพื่อทำการ Refund เงินคืนลูกค้า

---

## กรณีที่ 2: ผลการทำ Reconciliation ไม่ตรงกัน (Mismatch Detected)

### อาการและการตรวจพบ (Symptoms)
* หลังจากอัปโหลดไฟล์ `settlement.csv` บนหน้า Admin พบรายการแจ้งเตือนความไม่สอดคล้อง 4 ประเภท:
  1. `MISSING_IN_SYSTEM` (มีเงินเข้าที่ Provider แต่ไม่มีในระบบ)
  2. `MISSING_IN_PROVIDER` (ในระบบขึ้นว่าจ่ายแล้ว แต่ไม่มีในรายการโอนเงินของ Provider)
  3. `AMOUNT_MISMATCH` (ยอดเงินไม่ตรงกัน)
  4. `STATUS_MISMATCH` (สถานะไม่ตรงกัน)

### ขั้นตอนการตรวจสอบและแก้ไขแยกตามประเภท (Resolution by Type)

1. **`MISSING_IN_SYSTEM` (Webhook ตกหล่น):**
   * นำ `order_id` และ `event_id` จาก CSV ไปค้นหาในตาราง `webhook_events`
   * หากไม่พบ แสดงว่า Webhook ยิงมาไม่ถึงระบบ (เช่น เน็ตตัด หรือ Signature ผิด)
   * **การแก้ไข:** ดึงข้อมูล Webhook Payload จาก Dashboard ของ Payment Provider แล้วยิง Replay เข้ามาที่ `POST /api/v1/webhooks/payment` หรือให้ Finance ตรวจสอบและอนุมัติเติมมือ

2. **`MISSING_IN_PROVIDER` (ระบบเข้าใจผิดว่าจ่ายแล้ว / Fraud):**
   * **วิกฤตสูงสุด:** ตรวจสอบ `webhook_events` ทันทีว่า `event_id` นั้นถูกสร้างขึ้นได้อย่างไร ตรวจสอบว่า `PAYMENT_WEBHOOK_SECRET` รั่วไหลหรือไม่
   * **การแก้ไข:** ระงับบัญชีผู้ใช้งานที่เกี่ยวข้อง ตรวจสอบ Audit Logs และดำเนินการ Rollback/ปรับปรุงบัญชีร่วมกับฝ่ายการเงิน

3. **`AMOUNT_MISMATCH` (ลูกค้าจ่ายเงินขาดหรือเกิน):**
   * ระบบจะตั้งสถานะออเดอร์เป็น `NEEDS_REVIEW` โดยอัตโนมัติ (ไม่มีการเติมเกมให้)
   * **การแก้ไข:** ฝ่าย Finance ตรวจสอบยอดเงินจริงใน CSV หากลูกค้าโอนเกินหรือขาด ให้ทำการ **Refund เต็มจำนวน** ผ่านหน้า Admin เพื่อให้ลูกค้าทำรายการสั่งซื้อเข้ามาใหม่ให้ถูกต้อง

4. **`STATUS_MISMATCH` (เช่น Provider คืนเงินไปแล้ว แต่ในระบบยังเป็น `COMPLETED`):**
   * **การแก้ไข:** ตรวจสอบประวัติใน `order_events` หากพบว่ามีการคืนเงินที่ฝั่ง Provider จริง ให้ Finance บันทึกการ Refund ในระบบเพื่อปรับยอดบัญชีให้ตรงกัน

---

## กรณีที่ 3: ระบบเติมเกมปลายทาง (`mock-game`) ล่มทั้งวัน (Extended Outage)

### อาการและการตรวจพบ (Symptoms)
* `mock-game` ตอบ HTTP 500/502/504 หรือ Timeout 100% เป็นเวลานาน
* Queue `fulfillment` สะสมตัวสูงขึ้น และงานเริ่มไหลเข้า DLQ

### ขั้นตอนการรับมือ (Mitigation Plan)
1. **หยุดการรับออเดอร์ใหม่ชั่วคราว (Circuit Breaking / Maintenance Mode):**
   * เพื่อไม่ให้ลูกค้าจ่ายเงินเข้ามาแล้วต้องรอนานเกินไป ให้ Superadmin เข้าไปปิดการขายสินค้าชั่วคราว (ตั้งสถานะสินค้าเป็น Inactive หรือปรับ Stock เป็น 0) และขึ้น Banner แจ้งปิดปรับปรุงระบบเติมเกม
2. **ชะลอการทำงานของ Worker (ป้องกันการยิงถล่มระบบปลายทาง):**
   * ปรับค่า `WORKER_CONCURRENCY` ลดลง หรือหยุด Consumer ของคิว Fulfillment ชั่วคราว (แต่อย่าหยุดคิว Order Expiration เพื่อให้ออเดอร์ที่ยังไม่จ่ายเงินหมดอายุตามปกติ):
     ```bash
     docker compose stop worker
     ```
3. **การฟื้นฟูระบบเมื่อ `mock-game` กลับมาใช้งานได้ (Recovery):**
   * ทดสอบยิง `GET /healthz` ของระบบปลายทางจนมั่นใจว่าเสถียร
   * เปิดการทำงานของ Worker:
     ```bash
     docker compose start worker
     ```
   * Worker จะทำการกู้คืนงานที่ค้างใน `IN_PROGRESS` (เกิน 2 นาที) และทยอยดึงงานในคิวมาเติมให้ลูกค้าทั้งหมดโดยอัตโนมัติ (ด้วยระบบ Rate Limit และ Idempotency Reference ทำให้ไม่เกิดการเติมซ้ำ)
   * สำหรับรายการที่ไหลเข้า DLQ ไปแล้ว ให้ทีม Support สั่ง Re-queue ผ่านหน้า Admin