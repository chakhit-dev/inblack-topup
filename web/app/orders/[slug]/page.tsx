"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import {
  AlertCircle,
  ArrowLeft,
  ArrowRight,
  Check,
  CheckCircle2,
  Clock,
  Copy,
  CreditCard,
  Gamepad2,
  Loader2,
  Mail,
  QrCode,
  Receipt,
  RefreshCw,
  ShieldCheck,
  Wallet,
  XCircle,
} from "lucide-react";
import { useLanguage } from "@/components/button/context";
import { apiFetch, formatTHB, Order } from "@/lib/api";

const TERMINAL_STATUSES = ["COMPLETED", "FAILED", "EXPIRED", "CANCELLED"];

export default function OrderDetailPage() {
  const params = useParams<{ slug: string }>();
  const { lang, t } = useLanguage();

  const [order, setOrder] = useState<Order | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [secondsLeft, setSecondsLeft] = useState<number | null>(null);

  const fetchOrder = useCallback(
    async (silent = false) => {
      if (!params.slug) return;
      if (!silent) setLoading(true);

      try {
        const res = await apiFetch<Order | { data: Order }>(
          `/orders/${encodeURIComponent(params.slug)}`
        );
        const data = "data" in res && res.data ? res.data : (res as Order);
        setOrder(data);
        setError(null);
      } catch (err: unknown) {
        if (!silent) {
          setError(
            err instanceof Error
              ? err.message
              : t("ไม่พบข้อมูลคำสั่งซื้อนี้", "Order not found")
          );
        }
      } finally {
        if (!silent) setLoading(false);
      }
    },
    [params.slug, t]
  );

  // Initial fetch
  useEffect(() => {
    fetchOrder(false);
  }, [fetchOrder]);

  // Auto-polling every 3 seconds until order reaches a terminal status
  useEffect(() => {
    if (!order) return;
    const status = order.status?.toUpperCase();
    if (TERMINAL_STATUSES.includes(status)) return;

    const interval = setInterval(() => {
      fetchOrder(true);
    }, 3000);

    return () => clearInterval(interval);
  }, [order, fetchOrder]);

  // Countdown timer for PENDING_PAYMENT
  useEffect(() => {
    if (!order?.expires_at) return;

    const updateTimer = () => {
      const diff = Math.floor(
        (new Date(order.expires_at).getTime() - Date.now()) / 1000
      );
      setSecondsLeft(diff > 0 ? diff : 0);
    };

    updateTimer();
    const timer = setInterval(updateTimer, 1000);
    return () => clearInterval(timer);
  }, [order?.expires_at]);

  function handleCopyId() {
    if (!order?.id) return;
    navigator.clipboard.writeText(order.id);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  function formatCountdown(totalSec: number): string {
    const mins = Math.floor(totalSec / 60);
    const secs = totalSec % 60;
    return `${String(mins).padStart(2, "0")}:${String(secs).padStart(2, "0")}`;
  }

  function getStatusConfig(statusRaw: string) {
    const status = statusRaw?.toUpperCase();
    switch (status) {
      case "PENDING_PAYMENT":
        return {
          labelTh: "รอชำระเงิน",
          labelEn: "Pending Payment",
          descTh: "กรุณาชำระเงินภายในเวลาที่กำหนดเพื่อเริ่มเติมเกมอัตโนมัติ",
          descEn: "Complete payment before expiration to trigger instant delivery",
          badgeClass:
            "bg-amber-500/15 text-amber-600 ring-1 ring-amber-500/30 dark:text-amber-400",
          icon: <Clock className="h-5 w-5 text-amber-500" />,
        };
      case "PAID":
        return {
          labelTh: "ชำระเงินแล้ว",
          labelEn: "Paid",
          descTh: "ได้รับยอดชำระเรียบร้อย กำลังส่งคำสั่งไปยังคิวเติมเกม",
          descEn: "Payment confirmed, queuing order for fulfillment",
          badgeClass:
            "bg-sky-500/15 text-sky-600 ring-1 ring-sky-500/30 dark:text-sky-400",
          icon: <CheckCircle2 className="h-5 w-5 text-sky-500" />,
        };
      case "FULFILLING":
        return {
          labelTh: "กำลังเติมเกม",
          labelEn: "Fulfilling",
          descTh: "ระบบกำลังส่งไอเทมเข้าสู่บัญชีผู้เล่นของคุณ",
          descEn: "Delivering items directly to your game account",
          badgeClass:
            "bg-indigo-500/15 text-indigo-600 ring-1 ring-indigo-500/30 dark:text-indigo-400",
          icon: <Loader2 className="h-5 w-5 animate-spin text-indigo-500" />,
        };
      case "COMPLETED":
        return {
          labelTh: "เติมเกมสำเร็จ",
          labelEn: "Completed",
          descTh: "ทำรายการเสร็จสมบูรณ์ ไอเทมเข้าบัญชีเรียบร้อยแล้ว",
          descEn: "Order completed! Items have been credited to your account",
          badgeClass:
            "bg-emerald-500/15 text-emerald-600 ring-1 ring-emerald-500/30 dark:text-emerald-400",
          icon: <CheckCircle2 className="h-5 w-5 text-emerald-500" />,
        };
      case "EXPIRED":
        return {
          labelTh: "หมดเวลาชำระเงิน",
          labelEn: "Expired",
          descTh: "คำสั่งซื้อนี้เกินกำหนดชำระเงินและคืนสต็อกเข้าระบบแล้ว",
          descEn: "Payment window expired and reserved stock has been released",
          badgeClass:
            "bg-zinc-500/15 text-zinc-600 ring-1 ring-zinc-500/30 dark:text-zinc-400",
          icon: <Clock className="h-5 w-5 text-zinc-500" />,
        };
      case "FAILED":
      case "CANCELLED":
      default:
        return {
          labelTh: status === "CANCELLED" ? "ยกเลิกออเดอร์" : "ทำรายการไม่สำเร็จ",
          labelEn: status === "CANCELLED" ? "Cancelled" : "Failed",
          descTh: "เกิดข้อผิดพลาดระหว่างดำเนินการ กรุณาติดต่อแอดมินพร้อมแนบเลขที่ออเดอร์",
          descEn: "Order could not be processed. Please contact support with your Order ID",
          badgeClass:
            "bg-red-500/15 text-red-600 ring-1 ring-red-500/30 dark:text-red-400",
          icon: <XCircle className="h-5 w-5 text-red-500" />,
        };
    }
  }

  function renderPaymentIcon(method: string) {
    switch (method?.toLowerCase()) {
      case "truemoney":
        return <Wallet className="h-5 w-5 text-orange-500" />;
      case "card":
        return <CreditCard className="h-5 w-5 text-emerald-500" />;
      case "promptpay":
      default:
        return <QrCode className="h-5 w-5 text-sky-500" />;
    }
  }

  if (loading) {
    return (
      <main className="mx-auto flex min-h-[calc(100vh-8rem)] max-w-5xl items-center justify-center px-4 py-12">
        <div className="flex flex-col items-center gap-3 text-zinc-500 dark:text-zinc-400">
          <Loader2 className="h-8 w-8 animate-spin text-red-500" />
          <p className="text-sm">
            {t("กำลังโหลดข้อมูลคำสั่งซื้อ...", "Loading order details...")}
          </p>
        </div>
      </main>
    );
  }

  if (error || !order) {
    return (
      <main className="mx-auto flex min-h-[calc(100vh-8rem)] max-w-3xl flex-col items-center justify-center px-4 py-12 text-center">
        <div className="rounded-2xl border border-zinc-200 bg-white p-8 shadow-sm dark:border-zinc-800 dark:bg-zinc-900/60">
          <AlertCircle className="mx-auto h-12 w-12 text-red-500" />
          <h1 className="mt-4 text-xl font-bold text-zinc-900 dark:text-white">
            {t("ไม่พบคำสั่งซื้อ", "Order Not Found")}
          </h1>
          <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
            {error ||
              t("กรุณาตรวจสอบหมายเลขคำสั่งซื้ออีกครั้ง", "Please verify your Order ID")}
          </p>
          <div className="mt-6 flex flex-wrap justify-center gap-3">
            <Link
              href="/track"
              className="inline-flex items-center gap-2 rounded-xl border border-zinc-300 px-5 py-2.5 text-sm font-semibold text-zinc-800 transition hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-200 dark:hover:bg-zinc-800"
            >
              {t("ค้นหาออเดอร์อื่น", "Track Another Order")}
            </Link>
            <Link
              href="/"
              className="inline-flex items-center gap-2 rounded-xl bg-red-600 px-5 py-2.5 text-sm font-semibold text-white transition hover:bg-red-500"
            >
              <ArrowLeft className="h-4 w-4" />
              {t("กลับหน้าหลัก", "Back to Home")}
            </Link>
          </div>
        </div>
      </main>
    );
  }

  const statusUpper = order.status?.toUpperCase() || "PENDING_PAYMENT";
  const statusInfo = getStatusConfig(statusUpper);
  const isPending = statusUpper === "PENDING_PAYMENT";
  const isTerminal = TERMINAL_STATUSES.includes(statusUpper);

  return (
    <main className="mx-auto min-h-[calc(100vh-8rem)] max-w-5xl px-4 py-8">
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <Link
          href="/"
          className="inline-flex items-center gap-1.5 text-sm font-medium text-zinc-600 transition hover:text-red-600 dark:text-zinc-400 dark:hover:text-red-400"
        >
          <ArrowLeft className="h-4 w-4" />
          {t("กลับหน้าเลือกแพ็กเกจ", "Back to Packages")}
        </Link>

        <button
          type="button"
          onClick={() => fetchOrder(false)}
          className="inline-flex items-center gap-1.5 rounded-lg border border-zinc-200 bg-white px-3 py-1.5 text-xs font-semibold text-zinc-700 transition hover:bg-zinc-100 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800"
        >
          <RefreshCw
            className={`h-3.5 w-3.5 ${!isTerminal ? "animate-spin text-red-500" : ""}`}
          />
          <span>{t("อัปเดตสถานะ", "Refresh Status")}</span>
        </button>
      </div>

      <div className="mb-8 rounded-2xl border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900/70">
        <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
          <div className="flex items-start gap-3.5">
            <div className="mt-0.5 flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-zinc-100 dark:bg-zinc-800">
              {statusInfo.icon}
            </div>
            <div>
              <div className="flex flex-wrap items-center gap-2.5">
                <h1 className="text-lg font-bold text-zinc-900 dark:text-white">
                  {t(statusInfo.labelTh, statusInfo.labelEn)}
                </h1>
                <span
                  className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${statusInfo.badgeClass}`}
                >
                  {statusUpper}
                </span>
              </div>
              <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
                {t(statusInfo.descTh, statusInfo.descEn)}
              </p>
            </div>
          </div>

          {/* Countdown Box */}
          {isPending && secondsLeft !== null && (
            <div className="flex shrink-0 flex-col items-start rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-2.5 sm:items-end">
              <span className="text-[11px] font-medium text-amber-700 dark:text-amber-300">
                {t("กรุณาชำระเงินภายใน", "Pay within")}
              </span>
              <span className="font-mono text-xl font-extrabold text-amber-600 dark:text-amber-400">
                {formatCountdown(secondsLeft)}
              </span>
            </div>
          )}
        </div>

        <div className="mt-5 flex flex-wrap items-center justify-between gap-2 rounded-xl bg-zinc-50 px-4 py-2.5 text-xs dark:bg-zinc-950/80">
          <div className="flex items-center gap-2">
            <span className="text-zinc-500 dark:text-zinc-400">Order ID:</span>
            <code className="font-mono font-semibold text-zinc-900 dark:text-zinc-200">
              {order.id}
            </code>
          </div>
          <button
            type="button"
            onClick={handleCopyId}
            className="inline-flex items-center gap-1 font-semibold text-red-600 transition hover:text-red-500 dark:text-red-400"
          >
            {copied ? (
              <>
                <Check className="h-3.5 w-3.5" />
                <span>{t("คัดลอกแล้ว", "Copied")}</span>
              </>
            ) : (
              <>
                <Copy className="h-3.5 w-3.5" />
                <span>{t("คัดลอกรหัสออเดอร์", "Copy ID")}</span>
              </>
            )}
          </button>
        </div>
      </div>

      <div className="grid gap-8 lg:grid-cols-12">
        {/* คอลัมน์ซ้าย: ดำเนินการชำระเงิน & Timeline สถานะ */}
        <div className="space-y-6 lg:col-span-7">
          {/* Payment Action Card (แสดงเมื่อรอชำระเงิน) */}
          {isPending && (
            <div className="rounded-2xl border border-amber-500/30 bg-white p-6 shadow-sm dark:bg-zinc-900/70">
              <div className="flex items-center gap-2.5">
                {renderPaymentIcon(order.payment_method)}
                <div>
                  <h2 className="text-base font-bold text-zinc-900 dark:text-white uppercase">
                    {order.payment_method || "PROMPTPAY"}
                  </h2>
                  <p className="text-xs text-zinc-500 dark:text-zinc-400">
                    {t(
                      "ดำเนินการชำระเงินผ่าน Payment Gateway เพื่อเริ่มเติมเกมทันที",
                      "Proceed to Payment Gateway to complete your order"
                    )}
                  </p>
                </div>
              </div>

              <div className="mt-5 flex flex-col gap-3 rounded-xl bg-zinc-50 p-4 sm:flex-row sm:items-center sm:justify-between dark:bg-zinc-950/70">
                <div>
                  <span className="text-xs text-zinc-500 dark:text-zinc-400">
                    {t("ยอดที่ต้องชำระ", "Amount Due")}
                  </span>
                  <p className="text-2xl font-extrabold text-red-600 dark:text-red-500">
                    {formatTHB(order.total)}
                  </p>
                </div>

                <Link
                  href={`/mock-pay/${order.id}`}
                  className="inline-flex items-center justify-center gap-2 rounded-xl bg-red-600 px-6 py-3 text-sm font-bold text-white shadow-lg shadow-red-600/20 transition hover:bg-red-500"
                >
                  <span>{t("ไปหน้าชำระเงิน", "Proceed to Payment")}</span>
                  <ArrowRight className="h-4 w-4" />
                </Link>
              </div>
            </div>
          )}

          <div className="rounded-2xl border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900/70">
            <h2 className="mb-4 text-base font-bold text-zinc-900 dark:text-white">
              {t("ไทม์ไลน์สถานะคำสั่งซื้อ", "Order Status Timeline")}
            </h2>

            {order.events && order.events.length > 0 ? (
              <ol className="relative ml-3 space-y-6 border-l border-zinc-200 dark:border-zinc-800">
                {order.events.map((ev, idx) => {
                  const isLatest = idx === order.events!.length - 1;
                  return (
                    <li key={ev.id || idx} className="ml-6">
                      <span
                        className={`absolute -left-2 flex h-4 w-4 items-center justify-center rounded-full ring-4 ring-white dark:ring-zinc-900 ${isLatest
                            ? "bg-red-500"
                            : "bg-zinc-300 dark:bg-zinc-700"
                          }`}
                      />
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <span className="text-sm font-bold text-zinc-900 dark:text-white">
                          {ev.to_status}
                        </span>
                        <time className="text-xs text-zinc-500 dark:text-zinc-400">
                          {new Date(ev.created_at).toLocaleString("th-TH")}
                        </time>
                      </div>
                      {ev.reason && (
                        <p className="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
                          {ev.reason}
                        </p>
                      )}
                      {ev.actor && (
                        <span className="mt-1 inline-block rounded bg-zinc-100 px-1.5 py-0.5 font-mono text-[10px] text-zinc-600 dark:bg-zinc-800 dark:text-zinc-400">
                          actor: {ev.actor}
                        </span>
                      )}
                    </li>
                  );
                })}
              </ol>
            ) : (
              <div className="rounded-xl bg-zinc-50 p-4 text-xs text-zinc-500 dark:bg-zinc-950/60 dark:text-zinc-400">
                <p>
                  {t(
                    `สร้างคำสั่งซื้อเมื่อ ${new Date(order.created_at).toLocaleString("th-TH")}`,
                    `Order created at ${new Date(order.created_at).toLocaleString("en-US")}`
                  )}
                </p>
              </div>
            )}
          </div>
        </div>

        <div className="lg:col-span-5">
          <div className="sticky top-24 space-y-6 rounded-2xl border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900/70">
            <div className="flex items-center gap-2 border-b border-zinc-200 pb-4 dark:border-zinc-800">
              <Receipt className="h-5 w-5 text-red-500" />
              <h2 className="text-base font-bold text-zinc-900 dark:text-white">
                {t("รายละเอียดคำสั่งซื้อ", "Order Summary")}
              </h2>
            </div>

            <div className="space-y-3 text-sm">
              <div className="flex items-center justify-between">
                <span className="flex items-center gap-1.5 text-zinc-500 dark:text-zinc-400">
                  <Gamepad2 className="h-4 w-4" />
                  <span>Game UID</span>
                </span>
                <span className="font-mono font-bold text-zinc-900 dark:text-white">
                  {order.game_uid}
                </span>
              </div>

              <div className="flex items-center justify-between">
                <span className="flex items-center gap-1.5 text-zinc-500 dark:text-zinc-400">
                  <Mail className="h-4 w-4" />
                  <span>Email</span>
                </span>
                <span className="font-medium text-zinc-800 dark:text-zinc-200">
                  {order.customer_email}
                </span>
              </div>

              {order.voucher_code && (
                <div className="flex items-center justify-between">
                  <span className="text-zinc-500 dark:text-zinc-400">
                    {t("โค้ดส่วนลดที่ใช้", "Voucher Applied")}
                  </span>
                  <span className="rounded bg-emerald-500/15 px-2 py-0.5 font-mono text-xs font-bold text-emerald-600 dark:text-emerald-400">
                    {order.voucher_code}
                  </span>
                </div>
              )}
            </div>

            {order.items && order.items.length > 0 && (
              <div className="space-y-2.5 border-t border-zinc-200 pt-4 dark:border-zinc-800">
                <span className="block text-xs font-semibold tracking-wider text-zinc-500 uppercase dark:text-zinc-400">
                  {t("รายการแพ็กเกจ", "Items")}
                </span>
                {order.items.map((item, idx) => (
                  <div
                    key={`${item.sku}-${idx}`}
                    className="flex items-start justify-between gap-2 text-sm"
                  >
                    <div>
                      <p className="font-semibold text-zinc-900 dark:text-white">
                        {lang === "th" ? item.product_name_th : item.product_name_en}
                      </p>
                      <p className="text-xs text-zinc-500 dark:text-zinc-400">
                        {formatTHB(item.unit_price)} × {item.quantity}
                      </p>
                    </div>
                    <span className="font-semibold text-zinc-900 dark:text-zinc-200">
                      {formatTHB(item.total_price)}
                    </span>
                  </div>
                ))}
              </div>
            )}

            <div className="space-y-2 border-t border-zinc-200 pt-4 text-sm dark:border-zinc-800">
              <div className="flex justify-between text-zinc-600 dark:text-zinc-400">
                <span>{t("ยอดรวมย่อย", "Subtotal")}</span>
                <span>{formatTHB(order.subtotal)}</span>
              </div>

              {order.discount > 0 && (
                <div className="flex justify-between text-emerald-600 dark:text-emerald-400">
                  <span>{t("ส่วนลด", "Discount")}</span>
                  <span>-{formatTHB(order.discount)}</span>
                </div>
              )}

              {order.fee > 0 && (
                <div className="flex justify-between text-zinc-600 dark:text-zinc-400">
                  <span>{t("ค่าธรรมเนียม", "Processing Fee")}</span>
                  <span>{formatTHB(order.fee)}</span>
                </div>
              )}

              {order.vat > 0 && (
                <div className="flex justify-between text-zinc-600 dark:text-zinc-400">
                  <span>{t("ภาษีมูลค่าเพิ่ม (VAT)", "VAT")}</span>
                  <span>{formatTHB(order.vat)}</span>
                </div>
              )}

              <div className="flex items-baseline justify-between border-t border-zinc-200 pt-3 dark:border-zinc-800">
                <span className="font-bold text-zinc-900 dark:text-white">
                  {t("ยอดชำระสุทธิ", "Total Amount")}
                </span>
                <span className="text-xl font-extrabold text-red-600 dark:text-red-500">
                  {formatTHB(order.total)}
                </span>
              </div>
            </div>

            <div className="flex items-center gap-2 rounded-xl bg-zinc-50 p-3 text-xs text-zinc-500 dark:bg-zinc-950/70 dark:text-zinc-400">
              <ShieldCheck className="h-4 w-4 shrink-0 text-emerald-500" />
              <span>
                {t(
                  "ระบบตรวจสอบสถานะอัตโนมัติ ไม่จำเป็นต้องรีเฟรชหน้าจอ",
                  "Real-time status tracking enabled. No manual refresh needed."
                )}
              </span>
            </div>
          </div>
        </div>
      </div>
    </main>
  );
}