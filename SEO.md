# เอกสารแนวทางด้าน SEO และโครงสร้างหน้าเว็บ (SEO Strategy)

## 1. การปรับแต่งเพื่อเครื่องมือค้นหา (Technical SEO & Metadata)

ระบบ Frontend (Next.js App Router) ถูกออกแบบให้รองรับการจัดอันดับบน Search Engine สำหรับหน้าสาธารณะ (Public Pages) อย่างเต็มประสิทธิภาพ:

1. **Static Site Generation (SSG) & Incremental Static Regeneration (ISR):**
   * หน้าแรก (`/th`, `/en`) และหน้าแคตตาล็อกสินค้า ใช้การ Render ล่วงหน้า (Pre-rendering) เพื่อให้ Bot ของ Search Engine สามารถอ่านเนื้อหาได้ทันทีโดยไม่ต้องรอ JavaScript ทำงาน และได้ค่า Core Web Vitals (LCP, CLS) ในระดับสูง
2. **การรองรับหลายภาษา (Internationalization & `hreflang`):**
   * แยก URL ชัดเจนตามภาษา ได้แก่ `/th/...` และ `/en/...`
   * ฝังแท็ก `<link rel="alternate" hreflang="th" ... />`, `<link rel="alternate" hreflang="en" ... />` และ `x-default` ใน `<head>` ทุกหน้า เพื่อป้องกันปัญหา Duplicate Content ข้ามภาษา
3. **Structured Data (JSON-LD):**
   * ในหน้าสินค้า (`/products/[slug]`) มีการฝังข้อมูลโครงสร้าง `Product` และ `Offer` ตามมาตรฐาน Schema.org ระบุชื่อสินค้า, ราคา (`THB`), สถานะสินค้า (`InStock` / `OutOfStock` อัปเดตตาม Stock จริง) เพื่อให้แสดงผลแบบ Rich Snippets บนผลการค้นหา
4. **Dynamic Open Graph (OG) Images:**
   * สร้างรูปภาพตัวอย่างลิงก์อัตโนมัติ (Dynamic OG Image) ที่มีชื่อแพ็กเกจและราคาแสดงชัดเจนเมื่อมีการแชร์ลิงก์สินค้าลงบน Social Media

---

## 2. เหตุผลที่หน้า Tracking, My Orders และ Admin ไม่ควรถูก Index (`noindex`)

หน้าบางส่วนของระบบถูกกำหนดค่า `<meta name="robots" content="noindex, nofollow" />` และถูกนำออกจากไฟล์ `sitemap.xml` รวมถึงถูกบล็อกใน `robots.txt` ด้วยเหตุผลสำคัญ 4 ประการ ดังนี้:

1. **ความปลอดภัยและความเป็นส่วนตัวของข้อมูล (Security & PII Protection):**
   * หน้าติดตามออเดอร์ (`/track`, `/orders/[slug]`) และหน้าออเดอร์ของฉัน (`/my-orders`) แสดงข้อมูลธุรกรรมส่วนบุคคล หาก Search Engine เก็บ URL เหล่านี้ไปแสดงผล อาจเสี่ยงต่อการเปิดเผยข้อมูลธุรกรรมหรือ URL ที่มี Token/Order ID สู่สาธารณะ
2. **ป้องกันการเปิดเผยโครงสร้างระบบหลังบ้าน (Admin Attack Surface Reduction):**
   * หน้า `/admin` และ `/admin/login` มีไว้สำหรับเจ้าหน้าที่ภายในเท่านั้น การไม่ให้ Search Engine Index ช่วยลดโอกาสที่ผู้ไม่หวังดีจะค้นพบหน้า Login ของผู้ดูแลระบบผ่านการทำ Google Dorking
3. **คุณภาพของเนื้อหา (Thin / Empty Content):**
   * หน้า Tracking และ My Orders เป็นหน้า Dynamic ที่ต้องอาศัยการกรอกข้อมูลหรือการ Login จึงจะมีเนื้อหา หาก Bot เข้ามาเก็บข้อมูลจะเห็นเพียงหน้าฟอร์มว่างเปล่าหรือหน้า Error ซึ่งส่งผลเสียต่อคะแนนคุณภาพโดยรวมของโดเมน (Site Quality Score)
4. **การบริหารจัดการ Crawl Budget:**
   * เพื่อให้ Search Engine Bot ใช้ทรัพยากร (Crawl Budget) ไปกับการเก็บข้อมูลหน้าสินค้าและโปรโมชันที่สร้างรายได้ให้แก่แพลตฟอร์มอย่างเต็มที่ แทนที่จะเสียเวลาวิ่งเข้ามาในหน้าจัดการหลังบ้านหรือหน้าสถานะออเดอร์รายบุคคลที่มีจำนวนมหาศาล