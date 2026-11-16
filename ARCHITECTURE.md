# เอกสารสถาปัตยกรรมระบบ (System Architecture)

## 1. ภาพรวมสถาปัตยกรรม (High-Level Architecture)

ระบบ InBlack Top-Up ถูกออกแบบในรูปแบบ **Modular Distributed Architecture** แยกส่วนการทำงานระหว่างหน้าบ้าน (Frontend), ส่วนให้บริการ API แบบ Stateless หลาย Replica (API Layer), ส่วนประมวลผลเบื้องหลัง (Background Worker) และระบบจัดเก็บข้อมูลที่มีความแม่นยำสูง

```text
                        +-----------------------+
                        |   Client / Browser    |
                        +-----------+-----------+
                                    | HTTP / SSE
                                    v
                        +-----------------------+
                        |    Nginx (proxy)      |
                        |  Round-Robin Load Bal |
                        +----+-------------+----+
                             |             |
              +--------------+             +--------------+
              v                                           v
   +---------------------+                     +---------------------+
   |    Backend api-1    |                     |    Backend api-2    |
   |   (Go Stateless)    |                     |   (Go Stateless)    |
   +----------+----------+                     +----------+----------+
              |                                           |
              +---------------------+---------------------+
                                    |
          +-------------------------+-------------------------+
          |                         |                         |
          v                         v                         v
+-------------------+     +-------------------+     +-------------------+
|   PostgreSQL 16   |     |      Redis 7      |     |    Mailpit SMTP   |
| (Primary Storage, |     | (Streams Queue,   |     |   (OTP & Receipt) |
|  Locks, Ledger)   |     |  Pub/Sub, Cache)  |     +-------------------+
+---------+---------+     +---------+---------+
          ^                         ^
          |                         | Consume Jobs
          |               +---------+---------+
          +---------------+   Go Worker       |
                          | (Fulfillment, TTL,|
                          |  Reconciliation)  |
                          +---------+---------+
                                    | HTTP (Idempotent Ref)
                                    v
                          +-------------------+
                          |     mock-game     |
                          |  (Game Top-up API)|
                          +-------------------+
```

---

## 2. แผนภาพลำดับการทำงาน (Sequence Diagram: Payment to Fulfillment)

แผนภาพด้านล่างแสดงการทำงานตั้งแต่ลูกค้าชำระเงินผ่าน `mock-pay` การรับ Webhook ที่ `api-2` การส่งต่อให้ `worker` เติมเกมผ่าน `mock-game` และการอัปเดตสถานะแบบ Real-time กลับไปยังลูกค้าที่เชื่อมต่อ SSE อยู่กับ `api-1`

```text
Customer (SSE on api-1)      mock-pay           api-2             Postgres           Redis            worker          mock-game
         |                      |                 |                  |                 |                |                 |
         |                      |--POST Webhook-->|                  |                 |                |                 |
         |                      | (HMAC + RawBody)|                  |                 |                |                 |
         |                      |                 |--BEGIN TX------->|                 |                |                 |
         |                      |                 |--Insert Event--->|                 |                |                 |
         |                      |                 | (Deduplicate)    |                 |                |                 |
         |                      |                 |--Update Order--->|                 |                |                 |
         |                      |                 | (PAID->IN_QUEUE) |                 |                |                 |
         |                      |                 |--Insert Outbox-->|                 |                |                 |
         |                      |                 |--COMMIT TX------>|                 |                |                 |
         |                      |                 |                  |                 |                |                 |
         |                      |                 |--Enqueue Job---------------------->|                |                 |
         |                      |                 |--Publish SSE---------------------->|                |                 |
         |<--SSE: IN_QUEUE (via api-1 Pub/Sub)-----------------------------------------|                |                 |
         |                      |<--200 OK--------|                  |                 |                |                 |
         |                      |                 |                  |                 |<--Pull Job-----|                 |
         |                      |                 |                  |<--Lock & Mark---|                |                 |
         |                      |                 |                  | (IN_PROGRESS)   |                |                 |
         |                      |                 |                  |                 |                |--POST /topups-->|
         |                      |                 |                  |                 |                | (ref: ord_xxx)  |
         |                      |                 |                  |                 |                |<--200 OK--------|
         |                      |                 |                  |<--Update State--|                |                 |
         |                      |                 |                  | (COMPLETED)     |                |                 |
         |                      |                 |                  |                 |<--Publish SSE--|                 |
         |<--SSE: COMPLETED (via api-1 Pub/Sub)----------------------------------------|                |                 |
```

---

## 3. การจัดการความล้มเหลวในแต่ละจุด (Failure Handling Matrix)

| จุดที่เกิดความล้มเหลว (Failure Point) | ผลกระทบที่อาจเกิดขึ้น | กลไกการรับมือและการกู้คืนระบบ (Mitigation & Recovery) |
| :--- | :--- | :--- |
| **1. `api-1` หรือ `api-2` ล่มกะทันหัน (`docker kill`)** | Request ที่กำลังทำงานขาดหาย, การเชื่อมต่อ SSE หลุด | • Nginx ตรวจพบและส่งต่อ Traffic ไปยัง Replica ที่เหลือทันที<br>• Client ที่ต่อ SSE จะ Reconnect อัตโนมัติพร้อมส่ง Header `Last-Event-ID` เพื่อดึง Event ย้อนหลังจากตาราง `order_events` ไม่ให้ตกหล่น |
| **2. `worker` ตายกลางทางขณะเติมเกม** | ออเดอร์ค้างอยู่ที่สถานะ `IN_PROGRESS` และลูกค้าไม่ได้รับของ | • ระบบใช้ Lease Timeout (120 วินาที) มี Background Sweeper ตรวจสอบออเดอร์ที่ค้างใน `IN_PROGRESS` เกิน 2 นาที<br>• ก่อนยิงเติมเกมซ้ำ Worker จะเรียก `GET /topups/{reference}` ของ `mock-game` เพื่อเช็กว่าเติมสำเร็จไปแล้วหรือไม่ เพื่อป้องกันการเติมซ้ำ |
| **3. `mock-game` ตอบ Timeout ทั้งที่เติมสำเร็จแล้ว** | Worker เข้าใจว่าเติมไม่สำเร็จและพยายามยิงซ้ำ ทำให้เติมเกมเบิ้ล | • สร้าง Unique `fulfillment_reference` ผูกกับออเดอร์ใน DB ก่อนเรียก API ปลายทาง<br>• ทุกครั้งที่เกิด Timeout/5xx ในการ Retry รอบถัดไป Worker จะตรวจสอบสถานะกับ `GET /topups/{reference}` ก่อนเสมอ หากพบว่าสำเร็จแล้วจะเปลี่ยนสถานะเป็น `COMPLETED` ทันทีโดยไม่ยิง `POST /topups` ซ้ำ |
| **4. `mock-game` ล่มถาวร (5xx ต่อเนื่อง)** | งานค้างใน Queue และขัดขวางงานอื่น | • Worker ทำการ Retry แบบ Exponential Backoff + Jitter สูงสุด 5 ครั้ง<br>• หากยังไม่สำเร็จ จะย้ายงานไปที่ **Dead-Letter Queue (DLQ)** และเปลี่ยนสถานะออเดอร์เป็น `NEEDS_REVIEW` หรือ `FAILED` พร้อมแจ้งเตือนบน Admin Dashboard |
| **5. Payment Webhook ส่งซ้ำหรือสลับลำดับ** | เงินเข้าซ้ำ, สถานะย้อนกลับจาก `COMPLETED` เป็น `PAYMENT_FAILED` | • บันทึก `event_id` ลงตาราง `webhook_events` (Unique Constraint) ภายใน Transaction เดียวกับการเปลี่ยนสถานะ<br>• State Machine ปฏิเสธการเปลี่ยนสถานะย้อนกลับ (เช่น `PAID` จะไม่เปลี่ยนเป็น `PAYMENT_FAILED`) |
| **6. `redis` ล่มชั่วคราว** | คิวงานหยุดชะงัก, ระบบ Pub/Sub หยุดทำงาน | • Endpoint `/readyz` จะตอบ `503 Service Unavailable`<br>• ข้อมูลออเดอร์และสถานะทั้งหมดถูกบันทึกใน PostgreSQL เป็นหลัก (Source of Truth) เมื่อ Redis กลับมาทำงาน Outbox Poller จะส่งงานที่ค้างเข้า Queue ต่อได้ทันที |