### กรณีที่ 1: AI เกิดช่องโหว่ IDOR (คืนค่า `403 Forbidden`) และลืมตรวจสอบสถานะก่อนยิง Fulfillment ซ้ำ
* **สิ่งที่ AI สร้างผิด:** 
  1. ใน Endpoint `GET /api/v1/orders/{id}` AI เขียนโค้ดตรวจสอบว่าถ้า `order.UserID != currentUser.ID` ให้ตอบ `403 Forbidden` ซึ่งเป็นการยืนยันให้ผู้โจมตีรู้ว่าเลขออเดอร์นั้นมีอยู่จริงในระบบ (ขัดกับข้อกำหนด Security ข้อ 8.4)
  2. ในฝั่ง Worker AI เขียนโค้ด Retry ที่เมื่อ `mock-game` ตอบ Timeout ก็สั่งยิง `POST /topups` ซ้ำทันที ซึ่งทำให้เกิดการเติมเกมเบิ้ลเมื่อ `mock-game` อยู่ในโหมด `SUCCESS_BUT_TIMEOUT_RATE`
* **วิธีที่ตรวจพบ:** ตรวจสอบ Code Review เทียบกับ Security Checklist และทดสอบรัน Worker กับ `mock-game` โดยตั้งค่า `SUCCESS_BUT_TIMEOUT_RATE=1.0` พบว่ายอด `/stats` ของ `mock-game` ขึ้นว่าเติมสำเร็จ 2 ครั้งใน 1 ออเดอร์
* **วิธีแก้ไข:**
  1. เปลี่ยน HTTP Status Code กรณีไม่ใช่เจ้าของออเดอร์ให้คืนค่า `404 Not Found` เหมือนกับกรณีไม่พบออเดอร์ในฐานข้อมูล
  2. เพิ่มขั้นตอน **Idempotency Check** ใน Worker ก่อนทุกครั้งที่มีการ Retry ให้ยิง `GET /topups/{reference}` ไปถามสถานะจาก `mock-game` ก่อน หากพบว่าสถานะเป็น `COMPLETED` แล้ว ให้บันทึกสำเร็จทันทีโดยห้ามยิง `POST /topups` ซ้ำอีก

### กรณีที่ 2: การตรวจสอบลายเซ็น Webhook (HMAC-SHA256) จาก Parsed JSON แทน Raw Body
* **สิ่งที่ AI ทำผิด:** ในการสร้าง Handler `POST /api/v1/webhooks/payment` ช่วงแรก AI ใช้ `c.ShouldBindJSON(&payload)` อ่านข้อมูลก่อน แล้วค่อย `json.Marshal(payload)` กลับมาคำนวณ HMAC-SHA256 เพื่อเทียบกับ Header `X-IBM-Signature` รวมถึงใช้รูปแบบ Signature แบบ `sha256=<hex>` แทนรูปแบบ `t=<unix_seconds>,v1=<hex>` ตามสเปก
* **วิธีที่จับได้:** เมื่อทดสอบยิง Webhook ด้วย Payload ที่มีการจัดเรียง Key หรือเว้นวรรคต่างจาก Go Struct (`mock-pay` และชุดทดสอบของทีมตรวจ) พบว่า Signature ไม่ตรงและถูกปฏิเสธด้วย `401 Unauthorized` ทั้งที่ใช้ Secret ถูกต้อง
* **การแก้ไข:** ใช้ `io.ReadAll(c.Request.Body)` อ่าน Raw Bytes ดิบออกมาก่อนที่จะ Parse JSON, ตรวจสอบ Timestamp `t` ไม่ให้ห่างจากเวลาปัจจุบันเกิน 300 วินาทีเพื่อกัน Replay Attack และคำนวณ HMAC บนสตริง `"<t>.<raw_body>"` ตามสเปกหัวข้อที่ 6 เป๊ะๆ

### กรณีที่ 1: AI เขียน Integration Test ของโควตาสินค้า FLASH-SKIN-01 ขัดกับ Seed Data Spec
* **สิ่งที่ AI ทำผิด:**  ในไฟล์ backend/test/integration_test.go (TestIntegration_VoucherAndProductLimits) AI เขียนทดสอบให้ยิงสั่งซื้อสินค้า FLASH-SKIN-01 (ครั้งละ 1 ชิ้น) เพียง 2 ครั้ง แล้วคาดหวังให้ครั้งที่ 2 ถูกปฏิเสธด้วย 409 Conflict ทันที และเมื่อเทสไม่ผ่าน AI แนะนำให้แก้โค้ด Backend (postgres.go) เปลี่ยนค่า limit_per_email ของ FLASH-SKIN-01 จาก 2 เป็น 1
* **วิธีที่จับได้:**  เมื่อตรวจสอบเทียบกับเอกสารสเปกหัวข้อที่ 4 (Seed data บังคับ) พบว่าโจทย์กำหนดให้ FLASH-SKIN-01 จำกัด "2 ชิ้นต่ออีเมล" (ส่วนที่จำกัด 1 ชิ้นต่อออเดอร์คือ MONTHLY-PASS) แสดงว่าโค้ด Backend เขียนไว้ถูกต้องตามสเปกแล้ว หากแก้ Backend ตามที่ AI แนะนำจะทำให้ไม่ผ่านชุดทดสอบอัตโนมัติของทีมตรวจ
* **การแก้ไข:** คงค่า limit_per_email = 2 ใน Backend (postgres.go) ไว้ตามสเปกเดิม และแก้ไขไฟล์ integration_test.go ให้ยิงซื้อครั้งที่ 1 และ 2 สำเร็จ (201 Created) แล้วตรวจสอบว่าครั้งที่ 3 ถูกบล็อกด้วย 409 Conflict