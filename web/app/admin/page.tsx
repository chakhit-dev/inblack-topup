"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import {
    AlertCircle,
    CheckCircle2,
    ExternalLink,
    FileText,
    Loader2,
    LogOut,
    RefreshCw,
    RotateCcw,
    Scale,
    Search,
    ShieldCheck,
    ShoppingBag,
    Undo2,
} from "lucide-react";
import { useLanguage } from "@/components/button/context";
import { apiFetch, formatTHB, generateIdempotencyKey, Order } from "@/lib/api";

interface AdminUser {
    id?: string;
    email: string;
    role: string;
}

interface AuditLogItem {
    id: number;
    order_id: string;
    from_status: string | null;
    to_status: string;
    actor: string;
    reason: string;
    created_at: string;
}

type AdminTab = "orders" | "audit";

// ตรงตาม ENUM order_status ใน 000001_init_schema.up.sql
const ORDER_FILTERS = [
    "ALL",
    "PENDING_PAYMENT",
    "PAID",
    "IN_QUEUE",
    "IN_PROGRESS",
    "COMPLETED",
    "FAILED",
    "EXPIRED",
    "REFUNDED",
    "NEEDS_REVIEW",
];

function authHeaders(): Record<string, string> {
    if (typeof window === "undefined") return {};
    const token = localStorage.getItem("inblack_admin_token");
    return token ? { Authorization: `Bearer ${token}` } : {};
}

export default function AdminDashboardPage() {
    const router = useRouter();
    const { t } = useLanguage();

    const [adminUser, setAdminUser] = useState<AdminUser | null>(null);
    const [authChecked, setAuthChecked] = useState(false);

    const [activeTab, setActiveTab] = useState<AdminTab>("orders");
    const [loading, setLoading] = useState(false);
    const [bannerError, setBannerError] = useState<string | null>(null);
    const [bannerSuccess, setBannerSuccess] = useState<string | null>(null);

    // Orders Tab State
    const [orders, setOrders] = useState<Order[]>([]);
    const [statusFilter, setStatusFilter] = useState("ALL");
    const [orderSearch, setOrderSearch] = useState("");
    const [actionOrderId, setActionOrderId] = useState<string | null>(null);
    const [reconciling, setReconciling] = useState(false);

    // Audit Logs Tab State
    const [auditLogs, setAuditLogs] = useState<AuditLogItem[]>([]);

    useEffect(() => {
        try {
            const token = localStorage.getItem("inblack_admin_token");
            const rawUser = localStorage.getItem("inblack_admin_user");
            if (!token) {
                router.replace("/admin/login");
                return;
            }
            if (rawUser) {
                setAdminUser(JSON.parse(rawUser));
            } else {
                setAdminUser({ email: "admin@inblackm.com", role: "superadmin" });
            }
            setAuthChecked(true);
        } catch {
            router.replace("/admin/login");
        }
    }, [router]);

    const handleLogout = useCallback(() => {
        localStorage.removeItem("inblack_admin_token");
        localStorage.removeItem("inblack_admin_user");
        window.dispatchEvent(new Event("storage"));
        router.replace("/admin/login");
    }, [router]);

    const fetchTabData = useCallback(async () => {
        if (!authChecked) return;
        setLoading(true);
        setBannerError(null);

        try {
            if (activeTab === "orders") {
                const query =
                    statusFilter !== "ALL"
                        ? `?status=${encodeURIComponent(statusFilter)}`
                        : "";
                const res = await apiFetch<{ data?: Order[] }>(
                    `/admin/orders${query}`,
                    { headers: authHeaders() }
                );
                setOrders(Array.isArray(res) ? res : res.data || []);
            } else if (activeTab === "audit") {
                const res = await apiFetch<{ data?: AuditLogItem[] }>(
                    "/admin/audit-logs",
                    { headers: authHeaders() }
                );
                setAuditLogs(Array.isArray(res) ? res : res.data || []);
            }
        } catch (err: unknown) {
            const msg =
                err instanceof Error
                    ? err.message
                    : t("ไม่สามารถโหลดข้อมูลหลังบ้านได้", "Failed to load backoffice data");
            if (msg.toLowerCase().includes("unauthorized") || msg.includes("401")) {
                handleLogout();
                return;
            }
            setBannerError(msg);
        } finally {
            setLoading(false);
        }
    }, [activeTab, authChecked, handleLogout, statusFilter, t]);

    useEffect(() => {
        fetchTabData();
    }, [fetchTabData]);

    async function handleRetryFulfillment(orderId: string) {
        if (actionOrderId) return;
        setActionOrderId(orderId);
        setBannerError(null);
        setBannerSuccess(null);

        try {
            await apiFetch(
                `/admin/orders/${encodeURIComponent(orderId)}/retry-fulfillment`,
                {
                    method: "POST",
                    headers: authHeaders(),
                }
            );
            setBannerSuccess(
                t(
                    `ส่งคำสั่ง Retry Fulfillment สำหรับออเดอร์ ${orderId} เข้าคิวเรียบร้อยแล้ว`,
                    `Retry fulfillment triggered for order ${orderId}`
                )
            );
            await fetchTabData();
        } catch (err: unknown) {
            setBannerError(
                err instanceof Error
                    ? err.message
                    : t("ไม่สามารถส่งคำสั่งเติมเกมซ้ำได้", "Failed to retry fulfillment")
            );
        } finally {
            setActionOrderId(null);
        }
    }

    async function handleRefundOrder(orderId: string) {
        if (actionOrderId) return;
        setActionOrderId(orderId);
        setBannerError(null);
        setBannerSuccess(null);

        try {
            await apiFetch(`/admin/orders/${encodeURIComponent(orderId)}/refund`, {
                method: "POST",
                headers: {
                    ...authHeaders(),
                    "Idempotency-Key": generateIdempotencyKey("refund"),
                },
                body: JSON.stringify({ reason: "Admin manual refund" }),
            });
            setBannerSuccess(
                t(
                    `ดำเนินการคืนเงิน (Refund) ออเดอร์ ${orderId} เรียบร้อยแล้ว`,
                    `Refund processed for order ${orderId}`
                )
            );
            await fetchTabData();
        } catch (err: unknown) {
            setBannerError(
                err instanceof Error
                    ? err.message
                    : t("ไม่สามารถคืนเงินออเดอร์นี้ได้", "Failed to refund order")
            );
        } finally {
            setActionOrderId(null);
        }
    }

    async function handleReconcilePayments() {
        if (reconciling) return;
        setReconciling(true);
        setBannerError(null);
        setBannerSuccess(null);

        try {
            const todayUTC = new Date().toISOString().slice(0, 10);
            const records = orders
                .filter((o) =>
                    ["PAID", "IN_QUEUE", "IN_PROGRESS", "COMPLETED", "REFUNDED"].includes(
                        o.status?.toUpperCase()
                    )
                )
                .map((o) => ({
                    order_id: o.id,
                    amount: o.total,
                    reference: `SETTLE-${o.id}`,
                }));

            const res = await apiFetch<{
                summary?: {
                    matched_count: number;
                    mismatch_count: number;
                    unsettled_in_gw_count: number;
                };
            }>("/admin/reconciliation", {
                method: "POST",
                headers: authHeaders(),
                body: JSON.stringify({
                    settlement_date: todayUTC,
                    records,
                }),
            });

            const s = res.summary;
            setBannerSuccess(
                s
                    ? t(
                        `กระทบยอดวันที่ ${todayUTC} สำเร็จ: ตรงกัน ${s.matched_count} รายการ, ยอดไม่ตรง ${s.mismatch_count}, ค้างกระทบยอด ${s.unsettled_in_gw_count}`,
                        `Reconciled (${todayUTC}): ${s.matched_count} matched, ${s.mismatch_count} mismatch, ${s.unsettled_in_gw_count} unsettled`
                    )
                    : t("รันการตรวจสอบยอดเงินสำเร็จ", "Payment reconciliation completed")
            );
        } catch (err: unknown) {
            setBannerError(
                err instanceof Error
                    ? err.message
                    : t("รัน Reconciliation ไม่สำเร็จ", "Reconciliation failed")
            );
        } finally {
            setReconciling(false);
        }
    }

    function getStatusBadge(statusRaw: string) {
        const status = statusRaw?.toUpperCase();
        switch (status) {
            case "COMPLETED":
                return "bg-emerald-500/15 text-emerald-600 ring-1 ring-emerald-500/30 dark:text-emerald-400";
            case "PENDING_PAYMENT":
                return "bg-amber-500/15 text-amber-600 ring-1 ring-amber-500/30 dark:text-amber-400";
            case "PAID":
            case "IN_QUEUE":
            case "IN_PROGRESS":
                return "bg-sky-500/15 text-sky-600 ring-1 ring-sky-500/30 dark:text-sky-400";
            case "REFUNDED":
                return "bg-purple-500/15 text-purple-600 ring-1 ring-purple-500/30 dark:text-purple-400";
            case "EXPIRED":
                return "bg-zinc-500/15 text-zinc-600 ring-1 ring-zinc-500/30 dark:text-zinc-400";
            default:
                return "bg-red-500/15 text-red-600 ring-1 ring-red-500/30 dark:text-red-400";
        }
    }

    if (!authChecked) {
        return (
            <main className="mx-auto flex min-h-[calc(100vh-8rem)] max-w-6xl items-center justify-center px-4 py-12">
                <Loader2 className="h-8 w-8 animate-spin text-red-500" />
            </main>
        );
    }

    const role = adminUser?.role?.toLowerCase() || "viewer";
    const canRetry = role === "support" || role === "superadmin";
    const canRefund = role === "finance" || role === "superadmin";
    const canReconcile = role === "finance" || role === "superadmin";

    const filteredOrders = orders.filter((o) => {
        if (!orderSearch.trim()) return true;
        const q = orderSearch.trim().toLowerCase();
        return (
            o.id.toLowerCase().includes(q) ||
            o.game_uid?.toLowerCase().includes(q) ||
            o.customer_email?.toLowerCase().includes(q)
        );
    });

    return (
        <main className="mx-auto min-h-[calc(100vh-8rem)] max-w-7xl px-4 py-8">
            {/* Header Bar */}
            <div className="mb-8 flex flex-col justify-between gap-4 rounded-2xl border border-zinc-200 bg-white p-6 shadow-sm sm:flex-row sm:items-center dark:border-zinc-800 dark:bg-zinc-900/70">
                <div>
                    <div className="flex flex-wrap items-center gap-2.5">
                        <h1 className="text-xl font-extrabold text-zinc-900 sm:text-2xl dark:text-white">
                            {t("ระบบจัดการหลังบ้าน (Backoffice)", "Admin Backoffice")}
                        </h1>
                        {adminUser?.role && (
                            <span className="inline-flex items-center gap-1 rounded-full bg-red-500/15 px-3 py-0.5 text-xs font-bold text-red-600 uppercase ring-1 ring-red-500/30 dark:text-red-400">
                                <ShieldCheck className="h-3.5 w-3.5" />
                                {adminUser.role}
                            </span>
                        )}
                    </div>
                    <p className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
                        {adminUser?.email}
                    </p>
                </div>

                <div className="flex flex-wrap items-center gap-2.5">
                    {canReconcile && (
                        <button
                            type="button"
                            disabled={reconciling}
                            onClick={handleReconcilePayments}
                            className="inline-flex items-center gap-1.5 rounded-xl border border-sky-500/30 bg-sky-500/10 px-3.5 py-2 text-xs font-bold text-sky-600 transition hover:bg-sky-500/20 disabled:opacity-50 dark:text-sky-400"
                        >
                            {reconciling ? (
                                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            ) : (
                                <Scale className="h-3.5 w-3.5" />
                            )}
                            <span>{t("กระทบยอด (Reconcile)", "Reconcile")}</span>
                        </button>
                    )}

                    <button
                        type="button"
                        onClick={fetchTabData}
                        className="inline-flex items-center gap-1.5 rounded-xl border border-zinc-200 bg-zinc-50 px-3.5 py-2 text-xs font-bold text-zinc-700 transition hover:bg-zinc-100 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-200 dark:hover:bg-zinc-700"
                    >
                        <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
                        <span>{t("รีเฟรช", "Refresh")}</span>
                    </button>

                    <button
                        type="button"
                        onClick={handleLogout}
                        className="inline-flex items-center gap-1.5 rounded-xl border border-red-500/30 bg-red-500/10 px-3.5 py-2 text-xs font-bold text-red-600 transition hover:bg-red-500/20 dark:text-red-400"
                    >
                        <LogOut className="h-3.5 w-3.5" />
                        <span>{t("ออกจากระบบ", "Logout")}</span>
                    </button>
                </div>
            </div>

            {/* Feedback Banners */}
            {bannerError && (
                <div className="mb-6 flex items-start gap-2.5 rounded-xl border border-red-500/30 bg-red-500/10 p-4 text-xs text-red-600 dark:text-red-400">
                    <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
                    <span>{bannerError}</span>
                </div>
            )}

            {bannerSuccess && (
                <div className="mb-6 flex items-start gap-2.5 rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-4 text-xs text-emerald-600 dark:text-emerald-400">
                    <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
                    <span>{bannerSuccess}</span>
                </div>
            )}

            {/* Navigation Tabs */}
            <div className="mb-6 flex flex-wrap gap-2 border-b border-zinc-200 pb-4 dark:border-zinc-800">
                <button
                    type="button"
                    onClick={() => setActiveTab("orders")}
                    className={`inline-flex items-center gap-2 rounded-xl px-4 py-2.5 text-sm font-bold transition ${activeTab === "orders"
                            ? "bg-red-600 text-white shadow-sm"
                            : "bg-white text-zinc-600 hover:bg-zinc-100 dark:bg-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800"
                        }`}
                >
                    <ShoppingBag className="h-4 w-4" />
                    <span>{t("รายการคำสั่งซื้อ", "Orders & Operations")}</span>
                </button>

                <button
                    type="button"
                    onClick={() => setActiveTab("audit")}
                    className={`inline-flex items-center gap-2 rounded-xl px-4 py-2.5 text-sm font-bold transition ${activeTab === "audit"
                            ? "bg-red-600 text-white shadow-sm"
                            : "bg-white text-zinc-600 hover:bg-zinc-100 dark:bg-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800"
                        }`}
                >
                    <FileText className="h-4 w-4" />
                    <span>{t("ประวัติการทำงาน (Audit Logs)", "Audit Logs")}</span>
                </button>
            </div>

            {/* TAB 1: ORDERS */}
            {activeTab === "orders" && (
                <div className="space-y-4">
                    <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
                        <div className="flex flex-wrap gap-1.5">
                            {ORDER_FILTERS.map((st) => (
                                <button
                                    key={st}
                                    type="button"
                                    onClick={() => setStatusFilter(st)}
                                    className={`rounded-lg px-2.5 py-1.5 text-[11px] font-bold transition ${statusFilter === st
                                            ? "bg-zinc-900 text-white dark:bg-white dark:text-zinc-900"
                                            : "bg-zinc-100 text-zinc-600 hover:bg-zinc-200 dark:bg-zinc-900 dark:text-zinc-400 dark:hover:bg-zinc-800"
                                        }`}
                                >
                                    {st}
                                </button>
                            ))}
                        </div>

                        <div className="relative w-full sm:w-72">
                            <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-zinc-400" />
                            <input
                                type="text"
                                value={orderSearch}
                                onChange={(e) => setOrderSearch(e.target.value)}
                                placeholder={t("ค้นหา Order ID / UID / Email...", "Filter ID / UID / Email...")}
                                className="w-full rounded-xl border border-zinc-300 bg-white py-2 pr-3 pl-9 text-xs text-zinc-900 outline-none focus:border-red-500 dark:border-zinc-700 dark:bg-zinc-900 dark:text-white"
                            />
                        </div>
                    </div>

                    <div className="overflow-x-auto rounded-2xl border border-zinc-200 bg-white shadow-sm dark:border-zinc-800 dark:bg-zinc-900/70">
                        <table className="w-full text-left text-xs">
                            <thead className="border-b border-zinc-200 bg-zinc-50 text-zinc-500 uppercase dark:border-zinc-800 dark:bg-zinc-950/60 dark:text-zinc-400">
                                <tr>
                                    <th className="px-4 py-3.5">Order ID</th>
                                    <th className="px-4 py-3.5">Game UID / Email</th>
                                    <th className="px-4 py-3.5">{t("ยอดสุทธิ", "Total")}</th>
                                    <th className="px-4 py-3.5">{t("สถานะ", "Status")}</th>
                                    <th className="px-4 py-3.5">{t("เวลาที่สร้าง", "Created")}</th>
                                    <th className="px-4 py-3.5 text-right">{t("จัดการ", "Actions")}</th>
                                </tr>
                            </thead>
                            <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
                                {loading ? (
                                    <tr>
                                        <td colSpan={6} className="px-4 py-10 text-center">
                                            <Loader2 className="mx-auto h-6 w-6 animate-spin text-red-500" />
                                        </td>
                                    </tr>
                                ) : filteredOrders.length === 0 ? (
                                    <tr>
                                        <td
                                            colSpan={6}
                                            className="px-4 py-10 text-center text-zinc-500 dark:text-zinc-400"
                                        >
                                            {t("ไม่พบรายการคำสั่งซื้อ", "No orders found")}
                                        </td>
                                    </tr>
                                ) : (
                                    filteredOrders.map((ord) => {
                                        const st = ord.status?.toUpperCase();
                                        // ตาม State Machine: Retry ได้เฉพาะ FAILED และ Refund ได้เฉพาะ COMPLETED / FAILED / NEEDS_REVIEW
                                        const isRetryable = canRetry && st === "FAILED";
                                        const isRefundable =
                                            canRefund &&
                                            (st === "COMPLETED" || st === "FAILED" || st === "NEEDS_REVIEW");

                                        return (
                                            <tr
                                                key={ord.id}
                                                className="hover:bg-zinc-50/70 dark:hover:bg-zinc-800/40"
                                            >
                                                <td className="px-4 py-3.5 font-mono font-semibold text-zinc-900 dark:text-zinc-200">
                                                    {ord.id}
                                                </td>
                                                <td className="px-4 py-3.5">
                                                    <div className="font-mono font-bold text-zinc-900 dark:text-white">
                                                        {ord.game_uid}
                                                    </div>
                                                    <div className="text-zinc-500 dark:text-zinc-400">
                                                        {ord.customer_email}
                                                    </div>
                                                </td>
                                                <td className="px-4 py-3.5 font-bold text-red-600 dark:text-red-400">
                                                    {formatTHB(ord.total)}
                                                </td>
                                                <td className="px-4 py-3.5">
                                                    <span
                                                        className={`inline-block rounded-full px-2.5 py-0.5 text-[11px] font-semibold ${getStatusBadge(
                                                            ord.status
                                                        )}`}
                                                    >
                                                        {ord.status}
                                                    </span>
                                                </td>
                                                <td className="px-4 py-3.5 text-zinc-500 dark:text-zinc-400">
                                                    {new Date(ord.created_at).toLocaleString("th-TH")}
                                                </td>
                                                <td className="px-4 py-3.5 text-right">
                                                    <div className="inline-flex flex-wrap items-center justify-end gap-2">
                                                        {isRetryable && (
                                                            <button
                                                                type="button"
                                                                disabled={actionOrderId === ord.id}
                                                                onClick={() => handleRetryFulfillment(ord.id)}
                                                                className="inline-flex items-center gap-1 rounded-lg bg-amber-500/15 px-2.5 py-1.5 font-bold text-amber-600 transition hover:bg-amber-500/25 disabled:opacity-50 dark:text-amber-400"
                                                            >
                                                                {actionOrderId === ord.id ? (
                                                                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                                                                ) : (
                                                                    <RotateCcw className="h-3.5 w-3.5" />
                                                                )}
                                                                <span>Retry</span>
                                                            </button>
                                                        )}

                                                        {isRefundable && (
                                                            <button
                                                                type="button"
                                                                disabled={actionOrderId === ord.id}
                                                                onClick={() => handleRefundOrder(ord.id)}
                                                                className="inline-flex items-center gap-1 rounded-lg bg-purple-500/15 px-2.5 py-1.5 font-bold text-purple-600 transition hover:bg-purple-500/25 disabled:opacity-50 dark:text-purple-400"
                                                            >
                                                                <Undo2 className="h-3.5 w-3.5" />
                                                                <span>Refund</span>
                                                            </button>
                                                        )}

                                                        <Link
                                                            href={`/orders/${encodeURIComponent(ord.id)}`}
                                                            className="inline-flex items-center gap-1 rounded-lg border border-zinc-200 px-2.5 py-1.5 font-semibold text-zinc-700 transition hover:bg-zinc-100 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800"
                                                        >
                                                            <span>{t("ดูรายละเอียด", "View")}</span>
                                                            <ExternalLink className="h-3 w-3" />
                                                        </Link>
                                                    </div>
                                                </td>
                                            </tr>
                                        );
                                    })
                                )}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}

            {/* TAB 2: AUDIT LOGS */}
            {activeTab === "audit" && (
                <div className="overflow-x-auto rounded-2xl border border-zinc-200 bg-white shadow-sm dark:border-zinc-800 dark:bg-zinc-900/70">
                    <table className="w-full text-left text-xs">
                        <thead className="border-b border-zinc-200 bg-zinc-50 text-zinc-500 uppercase dark:border-zinc-800 dark:bg-zinc-950/60 dark:text-zinc-400">
                            <tr>
                                <th className="px-4 py-3.5">{t("เวลา", "Timestamp")}</th>
                                <th className="px-4 py-3.5">Order ID</th>
                                <th className="px-4 py-3.5">{t("การเปลี่ยนสถานะ", "Transition")}</th>
                                <th className="px-4 py-3.5">{t("ผู้ดำเนินการ", "Actor")}</th>
                                <th className="px-4 py-3.5">{t("เหตุผล", "Reason")}</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
                            {loading ? (
                                <tr>
                                    <td colSpan={5} className="px-4 py-10 text-center">
                                        <Loader2 className="mx-auto h-6 w-6 animate-spin text-red-500" />
                                    </td>
                                </tr>
                            ) : auditLogs.length === 0 ? (
                                <tr>
                                    <td
                                        colSpan={5}
                                        className="px-4 py-10 text-center text-zinc-500 dark:text-zinc-400"
                                    >
                                        {t("ยังไม่มีบันทึกประวัติการทำงาน", "No audit logs found")}
                                    </td>
                                </tr>
                            ) : (
                                auditLogs.map((log) => (
                                    <tr
                                        key={log.id}
                                        className="hover:bg-zinc-50/70 dark:hover:bg-zinc-800/40"
                                    >
                                        <td className="px-4 py-3.5 text-zinc-500 dark:text-zinc-400">
                                            {new Date(log.created_at).toLocaleString("th-TH")}
                                        </td>
                                        <td className="px-4 py-3.5 font-mono font-semibold text-zinc-900 dark:text-zinc-200">
                                            {log.order_id}
                                        </td>
                                        <td className="px-4 py-3.5 font-mono font-bold text-red-600 dark:text-red-400">
                                            {log.from_status ? `${log.from_status} → ` : ""}
                                            {log.to_status}
                                        </td>
                                        <td className="px-4 py-3.5 font-mono text-zinc-700 dark:text-zinc-300">
                                            {log.actor}
                                        </td>
                                        <td className="px-4 py-3.5 text-zinc-600 dark:text-zinc-400">
                                            {log.reason}
                                        </td>
                                    </tr>
                                ))
                            )}
                        </tbody>
                    </table>
                </div>
            )}
        </main>
    );
}