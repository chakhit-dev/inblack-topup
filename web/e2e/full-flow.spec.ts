import { test, expect } from '@playwright/test';

const MAILPIT_API = process.env.E2E_MAILPIT_URL || 'http://localhost:8025/api/v1';

test.describe('InBlack Top-Up E2E Flow', () => {
  test('purchase -> mock-pay -> real-time COMPLETED -> admin OTP login -> refund', async ({
    page,
    request,
  }) => {
    const customerEmail = `e2e_${Date.now()}@example.com`;
    const gameUID = 'UID-E2E-998877';

    // 1. ซื้อ: เปิดหน้าแรกผ่าน Nginx (:8080) แล้วเลือกแพ็กเกจที่มีสต็อกพร้อมขาย
    await page.goto('/');
    const productLink = page
      .locator('a[href*="/products/diamond-1000"], a[href*="/products/"]:not(:has-text("เหลือ 0 ชิ้น"))')
      .first();
    await expect(productLink).toBeVisible();
    await productLink.click();

    // กรอก Game UID (#game_uid) และ Email (#customer_email)
    const uidInput = page.locator('#game_uid');
    await expect(uidInput).toBeEnabled();
    await uidInput.fill(gameUID);

    const emailInput = page.locator('#customer_email');
    await emailInput.fill(customerEmail);

    await page.locator('button[type="submit"]').first().click();

    // รอเปลี่ยนไปหน้า Order (/orders/ORD-...)
    await page.waitForURL(/\/orders\/ORD-\d{13}-\d{3}/);
    const orderID = page.url().match(/(ORD-\d{13}-\d{3})/)![1];

    await expect(page.locator('body')).toContainText(/PENDING_PAYMENT/i);

    // 2. จ่าย: กดลิงก์ "ไปหน้าชำระเงิน" (<a> ที่ชี้ไป /checkout บนพอร์ต 9000)
    const payLink = page.locator('a[href*="/checkout"]').first();
    await expect(payLink).toBeVisible();
    await payLink.click();

    // รอโหลดหน้า Mock-Pay (localhost:9000) และกดปุ่มจ่ายสำเร็จ
    await page.waitForURL(/localhost:9000/);
    const succeedBtn = page
      .locator('button:has-text("จ่ายสำเร็จ"), button:has-text("Pay Success"), button:has-text("Success"), button[type="submit"]')
      .first();
    await expect(succeedBtn).toBeVisible();
    await succeedBtn.click();

    // Mock-Pay จะ Redirect กลับมาที่ return_url (/orders/ORD-...) อัตโนมัติ
    await page.waitForURL(new RegExp(`/orders/${orderID}`));

    // 3. เห็นสถานะเปลี่ยนเป็น COMPLETED แบบ Real-time (Auto-polling ทุก 3 วิ โดยไม่ต้องรีเฟรชหน้า)
    await expect(page.locator('body')).toContainText(/COMPLETED/i, { timeout: 25_000 });

    // 4. Admin Refund: เข้าสู่ระบบ Admin ด้วย OTP จาก Mailpit
    const adminEmail = 'admin@inblackm.com';
    await page.goto('/admin/login');

    await page.locator('input[type="email"]').first().fill(adminEmail);
    await page.locator('button[type="submit"]').first().click();

    // ดึงรหัส OTP 6 หลักล่าสุดจาก Mailpit API
    let otpCode = '';
    for (let i = 0; i < 12; i++) {
      await page.waitForTimeout(500);
      const listRes = await request.get(`${MAILPIT_API}/messages`);
      if (listRes.ok()) {
        const listJson = await listRes.json();
        const latest = listJson.messages?.find((m: { To?: Array<{ Address?: string }> }) =>
          m.To?.some((to) => to.Address?.toLowerCase() === adminEmail)
        );
        if (latest?.ID) {
          const msgRes = await request.get(`${MAILPIT_API}/message/${latest.ID}`);
          const msgJson = await msgRes.json();
          const match = (msgJson.Text || '').match(/\b(\d{6})\b/);
          if (match) {
            otpCode = match[1];
            break;
          }
        }
      }
    }
    expect(otpCode).toHaveLength(6);

    // กรอก OTP 6 หลักและเข้าสู่ Dashboard
    const otpInput = page.locator('input[maxlength="6"], input[type="text"]').first();
    await otpInput.fill(otpCode);
    await page.locator('button[type="submit"]').first().click();

    await page.waitForURL(/\/admin$/);
    await expect(page.locator('body')).toContainText(orderID);

    // กดปุ่ม Refund ออเดอร์
    const orderRow = page.locator(`tr:has-text("${orderID}"), div:has-text("${orderID}")`).first();
    await orderRow.locator('button:has-text("Refund"), button:has-text("คืนเงิน")').first().click();

    const confirmBtn = page
      .locator('[role="dialog"] button:has-text("Confirm"), [role="dialog"] button:has-text("ยืนยัน"), [role="dialog"] button:has-text("Refund")')
      .first();
    if (await confirmBtn.isVisible({ timeout: 3000 }).catch(() => false)) {
      const reasonInput = page.locator('[role="dialog"] input, [role="dialog"] textarea').first();
      if (await reasonInput.isVisible().catch(() => false)) {
        await reasonInput.fill('E2E automated refund verification');
      }
      await confirmBtn.click();
    }

    await expect(page.locator('body')).toContainText(/REFUNDED/i, { timeout: 15_000 });
  });
});