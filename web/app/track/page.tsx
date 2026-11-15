"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import {
    AlertCircle,
    ArrowRight,
    Clock,
    Gamepad2,
    Loader2,
    Mail,
    PackageSearch,
    Search,
} from "lucide-react";
import { useLanguage } from "@/components/button/context";
import { apiFetch, formatTHB, Order } from "@/lib/api";

export default function TrackOrderPage() {
    const router = useRouter();
    const { t } = useLanguage();

    const [mode, setMode] = useState<"id" | "email">("id");
    const [query, setQuery] = useState("");
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [matchedOrders, setMatchedOrders] = useState<Order[]>([]);

    async function handleSearch(e: React.FormEvent) {
        e.preventDefault();
        const trimmed = query.trim();
        if (!trimmed) {
            setError(
                mode === "id"
                    ? t("กรุณากรอกหมายเลขคำสั่งซื้อ (Order ID)", "Please enter your Order ID")
                    : t("กรุณากรอกอีเมลที่ใช้สั่งซื้อ", "Please enter your customer email")
            );
            return;
        }

        setLoading(true);
        setError(null);
        setMatchedOrders([]);

        try {
            if (mode === "id") {
                const res = await apiFetch<Order | { data: Order }>(
                    `/orders/${encodeURIComponent(trimmed)}`
                );
                const order = "data" in res && res.data ? res.data : (res as Order);
                if (!order?.id) {
                    throw new Error(t("ไม่พบคำสั่งซื้อนี้ในระบบ", "Order not found"));
                }
                router.push(`/orders/${encodeURIComponent(order.id)}`);
            } else {
                const res = await apiFetch<
                    Order[] | { data?: Order[]; orders?: Order[]; items?: Order[] }
                >("/orders/track", {
                    method: "POST",
                    body: JSON.stringify({ email: trimmed }),
                });

                const list = Array.isArray(res)
                    ? res
                    : res.data || res.orders || res.items || [];

                if (list.length === 0) {
                    setError(
                        t(
                            "ไม่พบประวัติคำสั่งซื้อที่เชื่อมโยงกับอีเมลนี้",
                            "No orders found associated with this email address"
                        )
                    );
                } else {
                    setMatchedOrders(list);
                }
            }
        } catch (err: unknown) {
            setError(
                err instanceof Error
                    ? err.message
                    : t("ไม่พบข้อมูลคำสั่งซื้อ กรุณาตรวจสอบอีกครั้ง", "Order not found. Please check and try again.")
            );
        } finally {
            setLoading(false);
        }
    }

    function getStatusBadge(statusRaw?: string) {
        const status = statusRaw?.toUpperCase() || "";
        switch (status) {
            case "COMPLETED":
                return "bg-emerald-500/15 text-emerald-600 ring-1 ring-emerald-500/30 dark:text-emerald-400";
            case "PENDING_PAYMENT":
                return "bg-amber-500/15 text-amber-600 ring-1 ring-amber-500/30 dark:text-amber-400";
            case "PAID":
            case "FULFILLING":
                return "bg-sky-500/15 text-sky-600 ring-1 ring-sky-500/30 dark:text-sky-400";
            case "EXPIRED":
                return "bg-zinc-500/15 text-zinc-600 ring-1 ring-zinc-500/30 dark:text-zinc-400";
            default:
                return "bg-red-500/15 text-red-600 ring-1 ring-red-500/30 dark:text-red-400";
        }
    }

    return (
        <main className="mx-auto min-h-[calc(100vh-8rem)] max-w-3xl px-4 py-10">
            <div className="mb-8 text-center">
                <div className="mx-auto mb-3 flex h-12 w-12 items-center justify-center rounded-2xl bg-red-500/10 text-red-600 ring-1 ring-red-500/20 dark:text-red-400">
                    <PackageSearch className="h-6 w-6" />
                </div>
                <h1 className="text-2xl font-extrabold tracking-tight text-zinc-900 sm:text-3xl dark:text-white">
                    {t("เช็กสถานะคำสั่งซื้อ", "Track Your Order")}
                </h1>
                <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
                    {t(
                        "ตรวจสอบสถานะการชำระเงินและการเติมเกมแบบเรียลไทม์ด้วย Order ID หรืออีเมลของคุณ",
                        "Check real-time payment and game top-up status using your Order ID or email"
                    )}
                </p>
            </div>

            <div className="rounded-2xl border border-zinc-200 bg-white p-6 shadow-sm sm:p-8 dark:border-zinc-800 dark:bg-zinc-900/70">
                <div className="mb-5 grid grid-cols-2 gap-2 rounded-xl bg-zinc-100 p-1 dark:bg-zinc-950/80">
                    <button
                        type="button"
                        onClick={() => {
                            setMode("id");
                            setQuery("");
                            setError(null);
                            setMatchedOrders([]);
                        }}
                        className={`flex items-center justify-center gap-2 rounded-lg py-2 text-xs font-bold transition sm:text-sm ${mode === "id"
                                ? "bg-white text-zinc-900 shadow-sm dark:bg-zinc-800 dark:text-white"
                                : "text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200"
                            }`}
                    >
                        <Search className="h-4 w-4" />
                        <span>{t("ค้นหาด้วย Order ID", "Search by Order ID")}</span>
                    </button>

                    <button
                        type="button"
                        onClick={() => {
                            setMode("email");
                            setQuery("");
                            setError(null);
                            setMatchedOrders([]);
                        }}
                        className={`flex items-center justify-center gap-2 rounded-lg py-2 text-xs font-bold transition sm:text-sm ${mode === "email"
                                ? "bg-white text-zinc-900 shadow-sm dark:bg-zinc-800 dark:text-white"
                                : "text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200"
                            }`}
                    >
                        <Mail className="h-4 w-4" />
                        <span>{t("ค้นหาด้วยอีเมล", "Search by Email")}</span>
                    </button>
                </div>

                <form onSubmit={handleSearch} className="space-y-4">
                    <div>
                        <label
                            htmlFor="track-query"
                            className="mb-1.5 block text-xs font-semibold text-zinc-700 dark:text-zinc-300"
                        >
                            {mode === "id"
                                ? t("หมายเลขคำสั่งซื้อ (Order ID)", "Order ID")
                                : t("อีเมลที่ใช้สั่งซื้อ (Customer Email)", "Customer Email")}
                        </label>
                        <div className="relative flex flex-col gap-3 sm:flex-row">
                            <input
                                id="track-query"
                                type={mode === "email" ? "email" : "text"}
                                value={query}
                                onChange={(e) => setQuery(e.target.value)}
                                placeholder={mode === "id" ? "เช่น ord_01J..." : "name@example.com"}
                                className="w-full rounded-xl border border-zinc-300 bg-zinc-50 px-4 py-3 text-sm text-zinc-900 transition outline-none focus:border-red-500 focus:bg-white focus:ring-2 focus:ring-red-500/20 dark:border-zinc-700 dark:bg-zinc-950 dark:text-white dark:focus:border-red-500 dark:focus:bg-zinc-900"
                            />
                            <button
                                type="submit"
                                disabled={loading}
                                className="inline-flex shrink-0 items-center justify-center gap-2 rounded-xl bg-red-600 px-6 py-3 text-sm font-bold text-white shadow-md shadow-red-600/20 transition hover:bg-red-500 disabled:opacity-50"
                            >
                                {loading ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                ) : (
                                    <Search className="h-4 w-4" />
                                )}
                                <span>{t("ค้นหา", "Track")}</span>
                            </button>
                        </div>
                    </div>

                    {error && (
                        <div className="flex items-start gap-2 rounded-xl border border-red-500/30 bg-red-500/10 p-3.5 text-xs text-red-600 dark:text-red-400">
                            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                            <span>{error}</span>
                        </div>
                    )}
                </form>
            </div>

            {matchedOrders.length > 0 && (
                <div className="mt-8 space-y-3">
                    <h2 className="text-sm font-bold text-zinc-900 dark:text-white">
                        {t(
                            `พบคำสั่งซื้อทั้งหมด ${matchedOrders.length} รายการ`,
                            `Found ${matchedOrders.length} order(s)`
                        )}
                    </h2>
                    <div className="space-y-3">
                        {matchedOrders.map((ord) => (
                            <Link
                                key={ord.id}
                                href={`/orders/${encodeURIComponent(ord.id)}`}
                                className="flex flex-col justify-between gap-3 rounded-2xl border border-zinc-200 bg-white p-4 shadow-sm transition hover:border-red-500/40 sm:flex-row sm:items-center dark:border-zinc-800 dark:bg-zinc-900/70 dark:hover:border-red-500/40"
                            >
                                <div className="space-y-1">
                                    <div className="flex flex-wrap items-center gap-2">
                                        <span className="font-mono text-sm font-bold text-zinc-900 dark:text-white">
                                            {ord.id}
                                        </span>
                                        <span
                                            className={`rounded-full px-2.5 py-0.5 text-[11px] font-semibold ${getStatusBadge(
                                                ord.status
                                            )}`}
                                        >
                                            {ord.status}
                                        </span>
                                    </div>
                                    <div className="flex flex-wrap items-center gap-3 text-xs text-zinc-500 dark:text-zinc-400">
                                        <span className="inline-flex items-center gap-1">
                                            <Gamepad2 className="h-3.5 w-3.5" />
                                            UID: {ord.game_uid}
                                        </span>
                                        <span className="inline-flex items-center gap-1">
                                            <Clock className="h-3.5 w-3.5" />
                                            {new Date(ord.created_at).toLocaleString("th-TH")}
                                        </span>
                                    </div>
                                </div>

                                <div className="flex items-center justify-between gap-4 sm:justify-end">
                                    <span className="text-base font-extrabold text-red-600 dark:text-red-500">
                                        {formatTHB(ord.total)}
                                    </span>
                                    <ArrowRight className="h-4 w-4 text-zinc-400" />
                                </div>
                            </Link>
                        ))}
                    </div>
                </div>
            )}
        </main>
    );
}