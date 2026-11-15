"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import {
    AlertCircle,
    ArrowLeft,
    CheckCircle2,
    Clock,
    CreditCard,
    Loader2,
    Lock,
    QrCode,
    ShieldCheck,
    Wallet,
    XCircle,
} from "lucide-react";
import { useLanguage } from "@/components/button/context";
import {
    apiFetch,
    formatTHB,
    generateIdempotencyKey,
    Order,
} from "@/lib/api";

export default function MockPaymentGatewayPage() {
    const params = useParams<{ orderId: string }>();
    const router = useRouter();
    const { t } = useLanguage();

    const [order, setOrder] = useState<Order | null>(null);
    const [loading, setLoading] = useState(true);
    const [fetchError, setFetchError] = useState<string | null>(null);

    const [secondsLeft, setSecondsLeft] = useState<number | null>(null);
    const [processingAction, setProcessingAction] = useState<
        "PAID" | "FAILED" | null
    >(null);
    const [actionError, setActionError] = useState<string | null>(null);

    useEffect(() => {
        if (!params.orderId) return;

        let active = true;
        setLoading(true);

        apiFetch<Order | { data: Order }>(
            `/orders/${encodeURIComponent(params.orderId)}`
        )
            .then((res) => {
                if (!active) return;
                const data = "data" in res && res.data ? res.data : (res as Order);
                setOrder(data);
                setFetchError(null);
            })
            .catch((err: unknown) => {
                if (!active) return;
                setFetchError(
                    err instanceof Error
                        ? err.message
                        : t("ไม่พบข้อมูลรายการชำระเงิน", "Payment session not found")
                );
            })
            .finally(() => {
                if (active) setLoading(false);
            });

        return () => {
            active = false;
        };
    }, [params.orderId, t]);

    useEffect(() => {
        if (!order?.expires_at) return;

        const tick = () => {
            const diff = Math.floor(
                (new Date(order.expires_at).getTime() - Date.now()) / 1000
            );
            setSecondsLeft(diff > 0 ? diff : 0);
        };

        tick();
        const timer = setInterval(tick, 1000);
        return () => clearInterval(timer);
    }, [order?.expires_at]);

    function formatCountdown(totalSec: number): string {
        const mins = Math.floor(totalSec / 60);
        const secs = totalSec % 60;
        return `${String(mins).padStart(2, "0")}:${String(secs).padStart(2, "0")}`;
    }

    async function triggerWebhook(outcome: "PAID" | "FAILED") {
        if (!order || processingAction) return;

        setProcessingAction(outcome);
        setActionError(null);

        const eventId = generateIdempotencyKey("evt");

        try {
            const res = await fetch("/api/mock-payment/webhook", {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                    "Idempotency-Key": eventId,
                },
                body: JSON.stringify({
                    event_id: eventId,
                    event_type:
                        outcome === "PAID" ? "payment.succeeded" : "payment.failed",
                    order_id: order.id,
                    status: outcome,
                    amount: order.total,
                    payment_method: order.payment_method,
                    occurred_at: new Date().toISOString(),
                }),
            });

            const data = await res.json().catch(() => ({}));

            if (!res.ok) {
                throw new Error(
                    data.detail ||
                    data.title ||
                    data.message ||
                    `Webhook failed (${res.status})`
                );
            }

            router.push(`/orders/${order.id}`);
        } catch (err: unknown) {
            setActionError(
                err instanceof Error
                    ? err.message
                    : t(
                        "เกิดข้อผิดพลาดในการส่งสัญญาณชำระเงิน",
                        "Failed to send payment callback"
                    )
            );
            setProcessingAction(null);
        }
    }

    if (loading) {
        return (
            <main className="mx-auto flex min-h-[calc(100vh-8rem)] max-w-lg items-center justify-center px-4 py-12">
                <div className="flex flex-col items-center gap-3 text-zinc-500 dark:text-zinc-400">
                    <Loader2 className="h-8 w-8 animate-spin text-red-500" />
                    <p className="text-sm">
                        {t(
                            "กำลังเชื่อมต่อ Payment Gateway...",
                            "Connecting to Payment Gateway..."
                        )}
                    </p>
                </div>
            </main>
        );
    }

    if (fetchError || !order) {
        return (
            <main className="mx-auto flex min-h-[calc(100vh-8rem)] max-w-lg flex-col items-center justify-center px-4 py-12 text-center">
                <div className="w-full rounded-2xl border border-zinc-200 bg-white p-8 shadow-sm dark:border-zinc-800 dark:bg-zinc-900/70">
                    <AlertCircle className="mx-auto h-12 w-12 text-red-500" />
                    <h1 className="mt-4 text-lg font-bold text-zinc-900 dark:text-white">
                        {t("ไม่พบรายการชำระเงิน", "Payment Session Not Found")}
                    </h1>
                    <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
                        {fetchError}
                    </p>
                    <Link
                        href="/"
                        className="mt-6 inline-flex items-center gap-2 rounded-xl bg-red-600 px-5 py-2.5 text-sm font-semibold text-white transition hover:bg-red-500"
                    >
                        <ArrowLeft className="h-4 w-4" />
                        {t("กลับหน้าหลัก", "Back to Home")}
                    </Link>
                </div>
            </main>
        );
    }

    const isPending = order.status?.toUpperCase() === "PENDING_PAYMENT";
    const isExpired =
        secondsLeft === 0 || order.status?.toUpperCase() === "EXPIRED";

    return (
        <main className="mx-auto min-h-[calc(100vh-8rem)] max-w-lg px-4 py-8">
            {/* Top Bar */}
            <div className="mb-5 flex items-center justify-between">
                <Link
                    href={`/orders/${order.id}`}
                    className="inline-flex items-center gap-1.5 text-sm font-medium text-zinc-600 transition hover:text-red-600 dark:text-zinc-400 dark:hover:text-red-400"
                >
                    <ArrowLeft className="h-4 w-4" />
                    {t("ย้อนกลับ", "Back")}
                </Link>

                <span className="inline-flex items-center gap-1.5 rounded-full bg-amber-500/15 px-3 py-1 text-xs font-bold text-amber-600 ring-1 ring-amber-500/30 dark:text-amber-400">
                    <Lock className="h-3 w-3" />
                    SANDBOX GATEWAY
                </span>
            </div>

            {/* Checkout Card */}
            <div className="overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-lg dark:border-zinc-800 dark:bg-zinc-900">
                {/* Header */}
                <div className="bg-gradient-to-r from-zinc-900 via-neutral-900 to-red-950 px-6 py-5 text-white">
                    <div className="flex items-center justify-between">
                        <div>
                            <p className="text-[11px] font-semibold tracking-wider text-zinc-400 uppercase">
                                InBlack Pay Checkout
                            </p>
                            <p className="mt-1 font-mono text-xs text-zinc-300">
                                #{order.id}
                            </p>
                        </div>
                        {secondsLeft !== null && isPending && (
                            <div className="flex items-center gap-1.5 rounded-xl bg-black/40 px-3 py-1.5 font-mono text-sm font-bold text-amber-400 ring-1 ring-white/10">
                                <Clock className="h-4 w-4" />
                                <span>{formatCountdown(secondsLeft)}</span>
                            </div>
                        )}
                    </div>

                    <div className="mt-5 flex items-end justify-between border-t border-white/10 pt-4">
                        <div>
                            <span className="text-xs text-zinc-400">
                                {t("ยอดชำระสุทธิ", "Total Amount")}
                            </span>
                            <p className="mt-0.5 text-3xl font-extrabold tracking-tight text-white">
                                {formatTHB(order.total)}
                            </p>
                        </div>
                        <div className="text-right text-xs text-zinc-300">
                            <span className="block text-zinc-400">Game UID</span>
                            <span className="font-mono font-semibold">{order.game_uid}</span>
                        </div>
                    </div>
                </div>

                {/* Body */}
                <div className="space-y-6 p-6">
                    {/* Payment Visual */}
                    <div className="flex flex-col items-center rounded-2xl border border-dashed border-zinc-300 bg-zinc-50 p-6 text-center dark:border-zinc-700 dark:bg-zinc-950/70">
                        {order.payment_method === "truemoney" ? (
                            <>
                                <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-orange-500/15 text-orange-500">
                                    <Wallet className="h-8 w-8" />
                                </div>
                                <p className="mt-3 text-sm font-bold text-zinc-900 dark:text-white">
                                    TrueMoney Wallet
                                </p>
                                <p className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
                                    {t(
                                        "เลือกสถานะการชำระเงินจำลองด้านล่างเพื่อดำเนินการต่อ",
                                        "Select a simulated payment outcome below to continue"
                                    )}
                                </p>
                            </>
                        ) : order.payment_method === "card" ? (
                            <>
                                <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-emerald-500/15 text-emerald-500">
                                    <CreditCard className="h-8 w-8" />
                                </div>
                                <p className="mt-3 text-sm font-bold text-zinc-900 dark:text-white">
                                    Credit / Debit Card
                                </p>
                                <p className="mt-1 font-mono text-xs text-zinc-500 dark:text-zinc-400">
                                    •••• •••• •••• 4242
                                </p>
                            </>
                        ) : (
                            <>
                                <div className="flex h-44 w-44 items-center justify-center rounded-2xl bg-white p-3 shadow-sm ring-1 ring-zinc-200">
                                    <QrCode className="h-32 w-32 text-zinc-900" />
                                </div>
                                <p className="mt-3 text-sm font-bold text-zinc-900 dark:text-white">
                                    Thai QR PromptPay
                                </p>
                                <p className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
                                    {t(
                                        "เลือกสถานะการชำระเงินจำลองด้านล่างเพื่อดำเนินการต่อ",
                                        "Select a simulated payment outcome below to continue"
                                    )}
                                </p>
                            </>
                        )}
                    </div>

                    {actionError && (
                        <div className="flex items-start gap-2 rounded-xl border border-red-500/30 bg-red-500/10 p-3.5 text-xs text-red-600 dark:text-red-400">
                            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                            <span>{actionError}</span>
                        </div>
                    )}

                    {/* Action Controls */}
                    {!isPending || isExpired ? (
                        <div className="rounded-xl border border-zinc-200 bg-zinc-50 p-4 text-center dark:border-zinc-800 dark:bg-zinc-950/60">
                            <p className="text-sm font-semibold text-zinc-800 dark:text-zinc-200">
                                {t(
                                    `รายการนี้อยู่ในสถานะ ${order.status} แล้ว`,
                                    `This order is already ${order.status}`
                                )}
                            </p>
                        </div>
                    ) : (
                        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                            <button
                                type="button"
                                disabled={processingAction !== null}
                                onClick={() => triggerWebhook("FAILED")}
                                className="flex items-center justify-center gap-2 rounded-xl border border-red-500/30 bg-red-500/10 px-4 py-3.5 text-sm font-bold text-red-600 transition hover:bg-red-500/20 disabled:opacity-50 dark:text-red-400"
                            >
                                {processingAction === "FAILED" ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                ) : (
                                    <XCircle className="h-4 w-4" />
                                )}
                                <span>{t("ชำระเงินล้มเหลว", "Payment Failed")}</span>
                            </button>

                            <button
                                type="button"
                                disabled={processingAction !== null}
                                onClick={() => triggerWebhook("PAID")}
                                className="flex items-center justify-center gap-2 rounded-xl bg-emerald-600 px-4 py-3.5 text-sm font-bold text-white shadow-md shadow-emerald-600/20 transition hover:bg-emerald-500 disabled:opacity-50"
                            >
                                {processingAction === "PAID" ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                ) : (
                                    <CheckCircle2 className="h-4 w-4" />
                                )}
                                <span>{t("ชำระเงินสำเร็จ", "Payment Success")}</span>
                            </button>
                        </div>
                    )}

                    {/* Footer Note */}
                    {/* <div className="flex items-center justify-center gap-1.5 border-t border-zinc-200 pt-4 text-[11px] text-zinc-500 dark:border-zinc-800 dark:text-zinc-400">
                        <ShieldCheck className="h-3.5 w-3.5 text-emerald-500" />
                        <span>
                            {t(
                                "เชื่อมต่อปลอดภัยด้วย HMAC-SHA256 Webhook",
                                "Secured with HMAC-SHA256 Webhook verification"
                            )}
                        </span>
                    </div> */}
                </div>
            </div>
        </main>
    );
}