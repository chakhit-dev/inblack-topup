"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import {
    AlertCircle,
    ArrowLeft,
    CheckCircle2,
    CreditCard,
    Gamepad2,
    Loader2,
    Mail,
    Minus,
    Package,
    Plus,
    QrCode,
    ShieldCheck,
    Sparkles,
    Tag,
    Wallet,
} from "lucide-react";
import { useLanguage } from "@/components/button/context";
import {
    apiFetch,
    formatTHB,
    generateIdempotencyKey,
    Order,
    Product,
} from "@/lib/api";

type PaymentMethod = "promptpay" | "truemoney" | "card";

export default function ProductDetailPage() {
    const params = useParams<{ slug: string }>();
    const router = useRouter();
    const { lang, t } = useLanguage();

    const [product, setProduct] = useState<Product | null>(null);
    const [loading, setLoading] = useState(true);
    const [fetchError, setFetchError] = useState<string | null>(null);

    const [quantity, setQuantity] = useState(1);
    const [gameUid, setGameUid] = useState("");
    const [customerEmail, setCustomerEmail] = useState("");
    const [voucherCode, setVoucherCode] = useState("");
    const [paymentMethod, setPaymentMethod] = useState<PaymentMethod>("promptpay");

    const [submitting, setSubmitting] = useState(false);
    const [submitError, setSubmitError] = useState<string | null>(null);
    const [idempotencyKey, setIdempotencyKey] = useState<string>("");

    useEffect(() => {
        setIdempotencyKey(generateIdempotencyKey("order"));
    }, [params.slug]);

    useEffect(() => {
        if (!params.slug) return;

        let active = true;
        setLoading(true);
        setFetchError(null);

        apiFetch<Product | { data: Product }>(
            `/products/${encodeURIComponent(params.slug)}`
        )
            .then((res) => {
                if (!active) return;
                const item = "data" in res && res.data ? res.data : (res as Product);
                setProduct(item);
            })
            .catch((err: unknown) => {
                if (!active) return;
                setFetchError(
                    err instanceof Error
                        ? err.message
                        : t("ไม่พบแพ็กเกจสินค้าที่ต้องการ", "Product package not found")
                );
            })
            .finally(() => {
                if (active) setLoading(false);
            });

        return () => {
            active = false;
        };
    }, [params.slug]);

    async function handleCreateOrder(e: React.FormEvent) {
        e.preventDefault();
        if (!product || submitting) return;

        setSubmitError(null);

        if (!gameUid.trim()) {
            setSubmitError(
                t("กรุณากรอก Game UID / Player ID", "Please enter your Game UID / Player ID")
            );
            return;
        }

        if (!customerEmail.trim() || !customerEmail.includes("@")) {
            setSubmitError(
                t("กรุณากรอกอีเมลให้ถูกต้อง", "Please enter a valid email address")
            );
            return;
        }

        setSubmitting(true);

        try {
            const payload: Record<string, unknown> = {
                game_uid: gameUid.trim(),
                customer_email: customerEmail.trim(),
                payment_method: paymentMethod,
                items: [
                    {
                        sku: product.sku,
                        quantity,
                    },
                ],
            };

            if (voucherCode.trim()) {
                payload.voucher_code = voucherCode.trim().toUpperCase();
            }

            const res = await apiFetch<Order | { data: Order }>("/orders", {
                method: "POST",
                headers: {
                    "Idempotency-Key": idempotencyKey || generateIdempotencyKey("order"),
                },
                body: JSON.stringify(payload),
            });

            const order = "data" in res && res.data ? res.data : (res as Order);

            if (order?.id) {
                router.push(`/orders/${order.id}`);
            } else {
                router.push("/track");
            }
        } catch (err: unknown) {
            setIdempotencyKey(generateIdempotencyKey("order"));
            setSubmitError(
                err instanceof Error
                    ? err.message
                    : t("ไม่สามารถสร้างคำสั่งซื้อได้ กรุณาลองใหม่", "Failed to create order. Please try again.")
            );
        } finally {
            setSubmitting(false);
        }
    }

    const paymentOptions: {
        id: PaymentMethod;
        titleTh: string;
        titleEn: string;
        descTh: string;
        descEn: string;
        icon: React.ReactNode;
    }[] = [
            {
                id: "promptpay",
                titleTh: "PromptPay QR",
                titleEn: "PromptPay QR",
                descTh: "สแกนจ่ายผ่านแอปธนาคาร ฟรีค่าธรรมเนียม",
                descEn: "Instant QR scan via any Thai bank app",
                icon: <QrCode className="h-5 w-5 text-sky-500" />,
            },
            {
                id: "truemoney",
                titleTh: "TrueMoney Wallet",
                titleEn: "TrueMoney Wallet",
                descTh: "ชำระเงินรวดเร็วผ่านทรูมันนี่ วอลเล็ท",
                descEn: "Fast checkout with TrueMoney Wallet",
                icon: <Wallet className="h-5 w-5 text-orange-500" />,
            },
            {
                id: "card",
                titleTh: "บัตรเครดิต / เดบิต",
                titleEn: "Credit / Debit Card",
                descTh: "รองรับ Visa, Mastercard และ JCB",
                descEn: "Supports Visa, Mastercard, and JCB",
                icon: <CreditCard className="h-5 w-5 text-emerald-500" />,
            },
        ];

    if (loading) {
        return (
            <main className="mx-auto flex min-h-[calc(100vh-8rem)] max-w-5xl items-center justify-center px-4 py-12">
                <div className="flex flex-col items-center gap-3 text-zinc-500 dark:text-zinc-400">
                    <Loader2 className="h-8 w-8 animate-spin text-red-500" />
                    <p className="text-sm">
                        {t("กำลังโหลดข้อมูลแพ็กเกจ...", "Loading package details...")}
                    </p>
                </div>
            </main>
        );
    }

    if (fetchError || !product) {
        return (
            <main className="mx-auto flex min-h-[calc(100vh-8rem)] max-w-3xl flex-col items-center justify-center px-4 py-12 text-center">
                <div className="rounded-2xl border border-zinc-200 bg-white p-8 shadow-sm dark:border-zinc-800 dark:bg-zinc-900/60">
                    <AlertCircle className="mx-auto h-12 w-12 text-red-500" />
                    <h1 className="mt-4 text-xl font-bold text-zinc-900 dark:text-white">
                        {t("ไม่พบข้อมูลสินค้า", "Product Not Found")}
                    </h1>
                    <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
                        {fetchError ||
                            t("แพ็กเกจนี้อาจถูกปิดการขายหรือไม่มีอยู่ในระบบ", "This package may be unavailable.")}
                    </p>
                    <Link
                        href="/"
                        className="mt-6 inline-flex items-center gap-2 rounded-xl bg-red-600 px-5 py-2.5 text-sm font-semibold text-white transition hover:bg-red-500"
                    >
                        <ArrowLeft className="h-4 w-4" />
                        {t("กลับหน้าเลือกแพ็กเกจ", "Back to Packages")}
                    </Link>
                </div>
            </main>
        );
    }

    const productName = lang === "th" ? product.name_th : product.name_en;
    const productDesc =
        lang === "th" ? product.description_th : product.description_en;

    const isUnlimitedStock = product.stock === null;
    const isOutOfStock =
        product.is_active === false || (!isUnlimitedStock && (product.stock ?? 0) <= 0);
    const maxQuantity = isUnlimitedStock ? 99 : Math.max(1, product.stock ?? 1);
    const estimatedSubtotal = product.price * quantity;

    return (
        <main className="mx-auto min-h-[calc(100vh-8rem)] max-w-5xl px-4 py-8">
            <div className="mb-6">
                <Link
                    href="/"
                    className="inline-flex items-center gap-1.5 text-sm font-medium text-zinc-600 transition hover:text-red-600 dark:text-zinc-400 dark:hover:text-red-400"
                >
                    <ArrowLeft className="h-4 w-4" />
                    {t("กลับไปหน้าแพ็กเกจทั้งหมด", "Back to All Packages")}
                </Link>
            </div>

            <div className="grid gap-8 lg:grid-cols-12">
                <div className="lg:col-span-5">
                    <div className="sticky top-24 overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-sm dark:border-zinc-800 dark:bg-zinc-900/70">
                        <div className="relative flex h-48 items-center justify-center bg-gradient-to-br from-zinc-900 via-neutral-900 to-red-950 p-6">
                            <div className="flex flex-col items-center gap-2 text-center">
                                <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-red-500/20 text-red-400 ring-1 ring-red-500/30">
                                    <Sparkles className="h-7 w-7" />
                                </div>
                                <span className="font-mono text-xs font-semibold tracking-wider text-zinc-400 uppercase">
                                    {product.sku}
                                </span>
                            </div>

                            <div className="absolute top-3 right-3">
                                {isOutOfStock ? (
                                    <span className="rounded-full bg-red-500/90 px-3 py-1 text-xs font-semibold text-white">
                                        {t("สินค้าหมด", "Out of Stock")}
                                    </span>
                                ) : isUnlimitedStock ? (
                                    <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/20 px-3 py-1 text-xs font-semibold text-emerald-300 ring-1 ring-emerald-500/40 backdrop-blur-sm">
                                        <Package className="h-3.5 w-3.5" />
                                        {t("พร้อมส่งทันที", "In Stock")}
                                    </span>
                                ) : (
                                    <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/20 px-3 py-1 text-xs font-semibold text-emerald-300 ring-1 ring-emerald-500/40 backdrop-blur-sm">
                                        <Package className="h-3.5 w-3.5" />
                                        {t(`คงเหลือ ${product.stock} รายการ`, `${product.stock} in stock`)}
                                    </span>
                                )}
                            </div>
                        </div>

                        <div className="p-6">
                            <h1 className="text-xl font-bold text-zinc-900 dark:text-white">
                                {productName}
                            </h1>

                            {productDesc && (
                                <p className="mt-2 text-sm leading-relaxed text-zinc-600 dark:text-zinc-400">
                                    {productDesc}
                                </p>
                            )}

                            <div className="mt-5 space-y-2.5 border-t border-zinc-200 pt-5 dark:border-zinc-800">
                                <div className="flex items-center justify-between text-sm">
                                    <span className="text-zinc-500 dark:text-zinc-400">
                                        {t("ราคาต่อหน่วย", "Unit Price")}
                                    </span>
                                    <span className="font-semibold text-zinc-900 dark:text-zinc-200">
                                        {formatTHB(product.price)}
                                    </span>
                                </div>

                                <div className="flex items-baseline justify-between">
                                    <span className="text-sm font-medium text-zinc-600 dark:text-zinc-300">
                                        {t(`ยอดรวม (${quantity} รายการ)`, `Subtotal (${quantity}x)`)}
                                    </span>
                                    <span className="text-2xl font-extrabold text-red-600 dark:text-red-500">
                                        {formatTHB(estimatedSubtotal)}
                                    </span>
                                </div>
                            </div>

                            <div className="mt-5 space-y-2 rounded-xl bg-zinc-50 p-3.5 text-xs text-zinc-600 dark:bg-zinc-950/70 dark:text-zinc-400">
                                <div className="flex items-center gap-2">
                                    <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-500" />
                                    <span>
                                        {t(
                                            "ระบบเติมอัตโนมัติเข้าบัญชีทันทีหลังชำระเงิน",
                                            "Instant automated delivery after payment"
                                        )}
                                    </span>
                                </div>
                                <div className="flex items-center gap-2">
                                    <ShieldCheck className="h-4 w-4 shrink-0 text-red-500" />
                                    <span>
                                        {t(
                                            "ระบบล็อกสต็อกป้องกันออเดอร์ชนกัน 100%",
                                            "Anti-oversell stock reservation guaranteed"
                                        )}
                                    </span>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>

                <div className="lg:col-span-7">
                    <form
                        onSubmit={handleCreateOrder}
                        className="space-y-6 rounded-2xl border border-zinc-200 bg-white p-6 shadow-sm dark:border-zinc-800 dark:bg-zinc-900/70 sm:p-8"
                    >
                        <div>
                            <h2 className="text-lg font-bold text-zinc-900 dark:text-white">
                                {t("กรอกข้อมูลสำหรับเติมเกม", "Enter Top-Up Details")}
                            </h2>
                            <p className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
                                {t(
                                    "กรุณาตรวจสอบ UID ให้ถูกต้องก่อนกดยืนยันคำสั่งซื้อ",
                                    "Please verify your UID carefully before confirming your order."
                                )}
                            </p>
                        </div>

                        {submitError && (
                            <div className="flex items-start gap-2.5 rounded-xl border border-red-500/30 bg-red-500/10 p-3.5 text-sm text-red-600 dark:text-red-400">
                                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                                <span>{submitError}</span>
                            </div>
                        )}

                        <div className="space-y-2">
                            <label
                                htmlFor="game_uid"
                                className="flex items-center gap-1.5 text-sm font-semibold text-zinc-800 dark:text-zinc-200"
                            >
                                <Gamepad2 className="h-4 w-4 text-red-500" />
                                <span>{t("Game UID / รหัสผู้เล่น", "Game UID / Player ID")}</span>
                                <span className="text-red-500">*</span>
                            </label>
                            <input
                                id="game_uid"
                                type="text"
                                required
                                disabled={isOutOfStock || submitting}
                                value={gameUid}
                                onChange={(e) => setGameUid(e.target.value)}
                                placeholder={t("เช่น 801234567 หรือ Player#TH1", "e.g. 801234567 or Player#TH1")}
                                className="w-full rounded-xl border border-zinc-300 bg-zinc-50 px-4 py-3 text-sm text-zinc-900 transition outline-none focus:border-red-500 focus:bg-white focus:ring-2 focus:ring-red-500/20 disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-white dark:focus:border-red-500 dark:focus:bg-zinc-950"
                            />
                        </div>

                        <div className="space-y-2">
                            <label
                                htmlFor="customer_email"
                                className="flex items-center gap-1.5 text-sm font-semibold text-zinc-800 dark:text-zinc-200"
                            >
                                <Mail className="h-4 w-4 text-red-500" />
                                <span>
                                    {t("อีเมลรับใบเสร็จและแจ้งเตือนสถานะ", "Email for Receipt & Order Updates")}
                                </span>
                                <span className="text-red-500">*</span>
                            </label>
                            <input
                                id="customer_email"
                                type="email"
                                required
                                disabled={isOutOfStock || submitting}
                                value={customerEmail}
                                onChange={(e) => setCustomerEmail(e.target.value)}
                                placeholder="you@example.com"
                                className="w-full rounded-xl border border-zinc-300 bg-zinc-50 px-4 py-3 text-sm text-zinc-900 transition outline-none focus:border-red-500 focus:bg-white focus:ring-2 focus:ring-red-500/20 disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-white dark:focus:border-red-500 dark:focus:bg-zinc-950"
                            />
                        </div>

                        <div className="grid gap-4 sm:grid-cols-2">
                            <div className="space-y-2">
                                <span className="block text-sm font-semibold text-zinc-800 dark:text-zinc-200">
                                    {t("จำนวนแพ็กเกจ", "Quantity")}
                                </span>
                                <div className="flex h-[46px] items-center justify-between rounded-xl border border-zinc-300 bg-zinc-50 px-2 dark:border-zinc-700 dark:bg-zinc-950">
                                    <button
                                        type="button"
                                        disabled={isOutOfStock || submitting || quantity <= 1}
                                        onClick={() => setQuantity((q) => Math.max(1, q - 1))}
                                        className="flex h-8 w-8 items-center justify-center rounded-lg text-zinc-600 transition hover:bg-zinc-200 disabled:opacity-40 dark:text-zinc-300 dark:hover:bg-zinc-800"
                                    >
                                        <Minus className="h-4 w-4" />
                                    </button>
                                    <span className="text-sm font-bold text-zinc-900 dark:text-white">
                                        {quantity}
                                    </span>
                                    <button
                                        type="button"
                                        disabled={isOutOfStock || submitting || quantity >= maxQuantity}
                                        onClick={() => setQuantity((q) => Math.min(maxQuantity, q + 1))}
                                        className="flex h-8 w-8 items-center justify-center rounded-lg text-zinc-600 transition hover:bg-zinc-200 disabled:opacity-40 dark:text-zinc-300 dark:hover:bg-zinc-800"
                                    >
                                        <Plus className="h-4 w-4" />
                                    </button>
                                </div>
                            </div>

                            <div className="space-y-2">
                                <label
                                    htmlFor="voucher_code"
                                    className="flex items-center gap-1.5 text-sm font-semibold text-zinc-800 dark:text-zinc-200"
                                >
                                    <Tag className="h-4 w-4 text-red-500" />
                                    <span>{t("โค้ดส่วนลด (ถ้ามี)", "Voucher Code")}</span>
                                </label>
                                <input
                                    id="voucher_code"
                                    type="text"
                                    disabled={isOutOfStock || submitting}
                                    value={voucherCode}
                                    onChange={(e) => setVoucherCode(e.target.value)}
                                    placeholder="WELCOME50"
                                    className="h-[46px] w-full rounded-xl border border-zinc-300 bg-zinc-50 px-4 text-sm uppercase text-zinc-900 transition outline-none focus:border-red-500 focus:bg-white focus:ring-2 focus:ring-red-500/20 disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-950 dark:text-white dark:focus:border-red-500 dark:focus:bg-zinc-950"
                                />
                            </div>
                        </div>

                        <div className="space-y-2.5">
                            <span className="block text-sm font-semibold text-zinc-800 dark:text-zinc-200">
                                {t("เลือกช่องทางชำระเงิน", "Select Payment Method")}
                            </span>
                            <div className="grid gap-3 sm:grid-cols-3">
                                {paymentOptions.map((option) => {
                                    const selected = paymentMethod === option.id;
                                    return (
                                        <button
                                            key={option.id}
                                            type="button"
                                            disabled={isOutOfStock || submitting}
                                            onClick={() => setPaymentMethod(option.id)}
                                            className={`flex flex-col items-start rounded-xl border p-3.5 text-left transition ${selected
                                                ? "border-red-500 bg-red-500/10 ring-1 ring-red-500"
                                                : "border-zinc-200 bg-zinc-50/70 hover:border-zinc-300 dark:border-zinc-800 dark:bg-zinc-950/60 dark:hover:border-zinc-700"
                                                } disabled:opacity-50`}
                                        >
                                            <div className="mb-2 flex w-full items-center justify-between">
                                                {option.icon}
                                                <span
                                                    className={`flex h-4 w-4 items-center justify-center rounded-full border ${selected
                                                        ? "border-red-500 bg-red-500"
                                                        : "border-zinc-300 dark:border-zinc-600"
                                                        }`}
                                                >
                                                    {selected && <span className="h-1.5 w-1.5 rounded-full bg-white" />}
                                                </span>
                                            </div>
                                            <span className="text-xs font-bold text-zinc-900 dark:text-white">
                                                {t(option.titleTh, option.titleEn)}
                                            </span>
                                            <span className="mt-1 line-clamp-2 text-[11px] leading-snug text-zinc-500 dark:text-zinc-400">
                                                {t(option.descTh, option.descEn)}
                                            </span>
                                        </button>
                                    );
                                })}
                            </div>
                        </div>

                        <button
                            type="submit"
                            disabled={isOutOfStock || submitting}
                            className="flex w-full items-center justify-center gap-2 rounded-xl bg-red-600 px-6 py-3.5 text-sm font-bold text-white shadow-lg shadow-red-600/20 transition hover:bg-red-500 disabled:cursor-not-allowed disabled:bg-zinc-400 disabled:shadow-none dark:disabled:bg-zinc-800 dark:disabled:text-zinc-500"
                        >
                            {submitting ? (
                                <>
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                    <span>{t("กำลังสร้างคำสั่งซื้อ...", "Creating Order...")}</span>
                                </>
                            ) : isOutOfStock ? (
                                <span>{t("สินค้าหมดชั่วคราว", "Out of Stock")}</span>
                            ) : (
                                <span>
                                    {t(
                                        `ยืนยันคำสั่งซื้อ • ${formatTHB(estimatedSubtotal)}`,
                                        `Confirm Order • ${formatTHB(estimatedSubtotal)}`
                                    )}
                                </span>
                            )}
                        </button>
                    </form>
                </div>
            </div>
        </main>
    );
}