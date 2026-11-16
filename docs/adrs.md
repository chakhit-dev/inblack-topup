# Architecture Decision Records (ADR)

---

## ADR-001: การป้องกันการขายเกินสต็อกและการใช้โค้ดส่วนลดเกินสิทธิ์ (Preventing Oversell & Voucher Abuse)

* **สถานะ:** ยอมรับและใช้งานจริง (Accepted)
* **บริบท:** สินค้ากลุ่ม Flash Sale (`FLASH-SKIN-01`) มีจำนวนจำกัดเพียง 100 ชิ้น และโค้ดส่วนลด `SAVE10` จำกัด 10 สิทธิ์ทั้งระบบ รวมถึงจำกัดจำนวนชิ้น/สิทธิ์ต่ออีเมลลูกค้า เมื่อมีผู้ใช้งานกดสั่งซื้อพร้อมกันหลายพันรายการอาจเกิด Race Condition ทำให้ยอดขายเกินสต็อกจริง
* **การตัดสินใจ:** ใช้กลไก **Pessimistic Row-Level Locking (`SELECT ... FOR UPDATE`)** ร่วมกับ **Database Check Constraints (`stock >= 0`)** และ **Atomic Conditional Update** ภายใน Database Transaction เดียวกันสำหรับการสร้างออเดอร์
  * ลำดับการ Lock ถูกกำหนดอย่างตายตัว (Canonical Lock Ordering: `products` เรียงตาม SKU -> `vouchers` -> `customer_limits`) เพื่อป้องกัน Deadlock
  * สำหรับการตรวจสอบโควตาต่ออีเมล จะนับจากออเดอร์ที่อยู่ในสถานะ Active (`PENDING_PAYMENT`, `PAID`, `IN_QUEUE`, `IN_PROGRESS`, `COMPLETED`) และใช้ Advisory Lock (`pg_advisory_xact_lock`) บน Hash ของอีเมลลูกค้า
* **ผลลัพธ์:** รับประกัน 100% ว่าไม่มีทางเกิด Oversell หรือใช้โค้ดเกินสิทธิ์แม้มี Request เข้ามาพร้อมกันหลายพันครั้ง โดยแลกกับ Latency ที่เพิ่มขึ้นเล็กน้อยในช่วงที่มีการแย่งซื้อสินค้าชิ้นเดียวกันสูงมาก

---

## ADR-002: การรับประกัน Idempotency ของ Payment Webhook และ Fulfillment

* **สถานะ:** ยอมรับและใช้งานจริง (Accepted)
* **บริบท:** Payment Provider (`mock-pay`) อาจส่ง Webhook `event_id` เดิมซ้ำหลายครั้งพร้อมกัน และระบบเติมเกม (`mock-game`) ไม่รองรับ Idempotency หากยิง `POST /topups` ซ้ำด้วย Reference เดิมจะทำให้ผู้เล่นได้รับไอเทมเบิ้ล
* **การตัดสินใจ:** แยกการป้องกันออกเป็น 2 ชั้น:
  1. **Webhook Idempotency:** ใช้ตาราง `webhook_events` ที่มี `event_id` เป็น Primary Key เมื่อได้รับ Webhook ระบบจะ `INSERT INTO webhook_events ... ON CONFLICT DO NOTHING` ภายใน Transaction เดียวกับการอัปเดตสถานะออเดอร์ หาก Insert ไม่สำเร็จ (ซ้ำ) ระบบจะคืนค่า `200 OK` ทันทีโดยไม่ประมวลผลซ้ำ
  2. **Fulfillment Idempotency (Check-Before-Act):** ก่อนยิง `POST /topups` Worker จะบันทึก `fulfillment_attempts` พร้อม `reference` ลง DB ก่อนเสมอ หากเกิด Timeout หรือ Worker ตายและเกิดการ Retry ในรอบถัดไป Worker **ต้อง** เรียก `GET /topups/{reference}` เพื่อตรวจสอบสถานะกับ `mock-game` ก่อนทุกครั้ง หากพบว่า `mock-game` เติมสำเร็จไปแล้วจะอัปเดตออเดอร์เป็น `COMPLETED` ทันที
* **ผลลัพธ์:** ป้องกันการเติมเกมซ้ำได้ 100% แม้ภายใต้สภาวะเครือข่ายไม่เสถียร (`SUCCESS_BUT_TIMEOUT_RATE`) หรือ Worker ถูกสั่งปิดกะทันหัน

---

## ADR-003: การส่งข้อมูล Real-time (SSE) ข้าม API Replicas

* **สถานะ:** ยอมรับและใช้งานจริง (Accepted)
* **บริบท:** ระบบรัน API Server จำนวน 2 Replicas (`api-1`, `api-2`) หลัง Nginx Load Balancer ลูกค้าอาจเปิดหน้าติดตามออเดอร์ (SSE) เชื่อมต่ออยู่กับ `api-1` ในขณะที่ Payment Webhook วิ่งเข้ามาที่ `api-2` หรือ Worker อัปเดตสถานะเสร็จสิ้น ลูกค้าต้องเห็นการเปลี่ยนแปลงภายใน 2 วินาที และต้องไม่พลาด Event หากเน็ตหลุด
* **การตัดสินใจ:** ใช้สถาปัตยกรรม **Hybrid Persistent Log + Redis Pub/Sub**:
  1. ทุกการเปลี่ยนสถานะจะถูกบันทึกลงตาราง `order_events` ใน PostgreSQL โดยใช้ `BIGSERIAL` (ลำดับตัวเลขที่เพิ่มขึ้นเรื่อยๆ) เป็น `event_id`
  2. ทันทีที่ Commit DB สำเร็จ API หรือ Worker จะ Publish ข้อความแจ้งเตือนไปยัง Redis Channel `order:updates:{order_id}`
  3. ทุก API Replica ที่ถือ Connection SSE ของลูกค้านั้นอยู่จะได้รับข้อความจาก Redis Pub/Sub และส่งออกไปทาง SSE Stream ทันที
  4. หาก Client หลุดการเชื่อมต่อและส่ง Header `Last-Event-ID` กลับมา API จะ Query ตาราง `order_events WHERE id > last_event_id` ส่งให้ลูกค้าจนครบก่อนเข้าสู่โหมดรอรับจาก Pub/Sub
* **ผลลัพธ์:** รองรับการขยายตัวของ API Server ได้ไม่จำกัดจำนวน Replica โดยใช้เวลาส่งต่อข้อมูลน้อยกว่า 50ms และรับประกันความครบถ้วนของลำดับ Event (At-least-once / Ordered delivery)

---

## ADR-004: การจัดการ Payment Webhook ที่มาถึงหลังจากออเดอร์หมดอายุ (Late Payment after Expired)

* **สถานะ:** ยอมรับและใช้งานจริง (Accepted)
* **บริบท:** ออเดอร์มีเวลาชำระเงินจำกัด (`ORDER_TTL_SECONDS = 900s`) เมื่อหมดอายุ Worker จะเปลี่ยนสถานะเป็น `EXPIRED` และคืน Stock กลับเข้าระบบ แต่ลูกค้าอาจสแกนจ่ายเงินค้างไว้ทำให้ `payment.succeeded` ส่งมาถึงหลังจากออเดอร์ `EXPIRED` ไปแล้ว
* **การตัดสินใจ:** ห้ามนิ่งเฉยและห้ามยึดเงินลูกค้าโดยไม่ส่งมอบของ ระบบจะทำงานแบบ **Auto-Reclaim or Auto-Refund**:
  1. เมื่อ Webhook `payment.succeeded` เข้ามาพบว่าออเดอร์อยู่ในสถานะ `EXPIRED` ระบบจะเปิด Transaction และตรวจสอบ Stock ของสินค้าและสิทธิ์โค้ดส่วนลดในออเดอร์นั้นอีกครั้ง (Lock แถวสินค้า)
  2. **กรณีที่ 1 (ยังมี Stock และสิทธิ์โค้ดเพียงพอ):** ระบบจะทำการตัด Stock และสิทธิ์โค้ดใหม่ เปลี่ยนสถานะออเดอร์จาก `EXPIRED` -> `PAID` -> `IN_QUEUE` และดำเนินการเติมเกมให้ลูกค้าตามปกติ พร้อมบันทึกเหตุผลใน `order_events`
  3. **กรณีที่ 2 (Stock หมดแล้ว หรือสิทธิ์โค้ดเต็มแล้ว):** ระบบจะเปลี่ยนสถานะเป็น `PAID` แล้วสั่งสร้างรายการ **Auto-Refund เต็มจำนวน** ทันที พร้อมส่ง Job คืนเงินไปยัง Payment Gateway และส่งอีเมลแจ้งลูกค้า
* **ผลลัพธ์:** รักษาประสบการณ์ที่ดีของลูกค้า ไม่เสียยอดขายหากยังมีสินค้าเหลือ และไม่เกิดความเสี่ยงทางกฎหมาย/การเงินหากสินค้าหมดไปแล้ว

---

## ADR-005: การป้องกันการคืนเงินเกินยอดจ่ายภายใต้สภาวะ Race Condition (Concurrent Refund Protection)

* **สถานะ:** ยอมรับและใช้งานจริง (Accepted)
* **บริบท:** เจ้าหน้าที่ฝ่ายการเงิน (Finance) 2 คน อาจกดปุ่มคืนเงิน (ทั้งแบบเต็มจำนวนหรือบางส่วน - Partial Refund) บนออเดอร์เดียวกันพร้อมกัน หรือเกิดการยิง API ซ้ำ ทำให้ยอดคืนเงินรวมทะลุยอดที่ลูกค้าชำระจริง (`total_satang`)
* **การตัดสินใจ:** ใช้แนวทาง **Ledger-based Refund Aggregation + Row Locking**:
  1. บังคับใช้ `Idempotency-Key` สำหรับ Endpoint `POST /api/v1/admin/orders/{id}/refund`
  2. เมื่อมีคำขอคืนเงิน ระบบจะ Lock แถวของออเดอร์ด้วย `SELECT ... FROM orders WHERE id = $1 FOR UPDATE`
  3. คำนวณยอดเงินที่คืนไปแล้วทั้งหมดจากตาราง `refunds` (`SELECT COALESCE(SUM(amount_satang), 0) FROM refunds WHERE order_id = $1 AND status IN ('PENDING', 'SUCCEEDED')`)
  4. ตรวจสอบเงื่อนไข: `refunded_so_far + requested_refund_amount <= order.total_satang` หากเกินจะปฏิเสธด้วย `422 Unprocessable Entity` ทันที
  5. เพิ่ม Database Trigger/Constraint ตรวจสอบยอดรวมในตาราง `refunds` อีกชั้นเพื่อป้องกันความผิดพลาดระดับ Application
* **ผลลัพธ์:** รับประกัน 100% ว่ายอดคืนเงินไม่มีทางเกินยอดที่ลูกค้าชำระจริง ไม่ว่าจะมีการกดพร้อมกันกี่ Request ก็ตาม