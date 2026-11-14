"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { Gamepad2, Zap, Flame, Loader2, AlertCircle } from "lucide-react";
import { apiFetch, Product } from "@/lib/api";
import { useLanguage } from "@/components/button/context";

const GRADIENTS = [
    "from-red-500/20 via-fuchsia-500/10 to-zinc-100 dark:from-red-600/30 dark:via-fuchsia-600/20 dark:to-zinc-900",
    "from-rose-500/20 via-zinc-100 to-zinc-200/70 dark:from-rose-600/25 dark:via-zinc-900 dark:to-zinc-950",
    "from-amber-500/20 via-zinc-100 to-zinc-200/70 dark:from-amber-500/25 dark:via-zinc-900 dark:to-zinc-950",
    "from-sky-500/20 via-zinc-100 to-zinc-200/70 dark:from-sky-500/25 dark:via-zinc-900 dark:to-zinc-950",
];

export default function Home() {
    const { lang, t } = useLanguage();
    const [loadingProducts, setLoadingProducts] = useState(true);
    const [products, setProducts] = useState<Product[]>([]);
    const [errorMsg, setErrorMsg] = useState<string | null>(null);

    useEffect(() => {
        apiFetch<any>("/products")
            .then((res) => {
                const list = Array.isArray(res) ? res : (res.data || []);
                setProducts(list);
            })
            .catch((err) => setErrorMsg(err.message))
            .finally(() => setLoadingProducts(false));
    }, []);

    return (
        <main className="mx-auto min-h-screen max-w-6xl px-4 py-8 text-zinc-900 dark:text-zinc-100">
            <div className="flex flex-col gap-6">
                <div className="flex items-end justify-between">
                    <div>
                        <h1 className="text-2xl font-bold tracking-tight text-zinc-900 dark:text-white">
                            {t("แพ็กเกจและเกมที่ให้บริการ", "Available Games & Packages")}
                        </h1>
                        <p className="mt-1 text-sm text-zinc-600 dark:text-zinc-400">
                            {t(
                                "เลือกแพ็กเกจที่คุณต้องการเติมเงิน รวดเร็ว ปลอดภัย เข้าทันที",
                                "Select a package to top up — fast, secure, and instant delivery"
                            )}
                        </p>
                    </div>
                </div>

                {errorMsg && (
                    <div className="flex items-center gap-2 rounded-xl border border-red-500/30 bg-red-500/10 p-4 text-sm text-red-600 dark:text-red-300">
                        <AlertCircle className="h-4 w-4 shrink-0" />
                        <span>{t("โหลดข้อมูลสินค้าไม่สำเร็จ:", "Failed to load products:")} {errorMsg}</span>
                    </div>
                )}

                {loadingProducts ? (
                    <div className="flex h-64 items-center justify-center">
                        <Loader2 className="h-8 w-8 animate-spin text-red-500 dark:text-red-400" />
                    </div>
                ) : (
                    <div className="grid grid-cols-2 gap-4 sm:gap-5 md:grid-cols-4">
                        {products.map((product, index) => {
                            const gradient = GRADIENTS[index % GRADIENTS.length];
                            const isLimited = product.stock !== null;
                            const productName = lang === "th" ? product.name_th : (product.name_en || product.name_th);

                            return (
                                <Link
                                    key={product.id}
                                    href={`/products/${product.slug}`}
                                    className="group relative flex h-80 flex-col overflow-hidden rounded-2xl border border-zinc-200 bg-white transition-all duration-300 hover:-translate-y-1 hover:border-red-500/60 dark:border-zinc-800 dark:bg-zinc-900/50"
                                >
                                    <div
                                        className={`relative flex flex-1 items-center justify-center overflow-hidden bg-linear-to-br ${gradient} p-4`}
                                    >
                                        {isLimited && (
                                            <span className="absolute right-3 top-3 inline-flex items-center gap-1 rounded-full border border-red-400/30 bg-red-600/90 px-2.5 py-0.5 text-[11px] font-semibold text-white backdrop-blur-sm">
                                                <Flame className="h-3 w-3 text-amber-300" />
                                                {t(`เหลือ ${product.stock} ชิ้น`, `${product.stock} left`)}
                                            </span>
                                        )}

                                        <div className="flex h-16 w-16 items-center justify-center rounded-2xl border border-zinc-200/80 bg-white/80 text-red-500 transition-transform duration-300 group-hover:scale-110 group-hover:text-red-600 dark:border-white/10 dark:bg-zinc-950/60 dark:text-red-400 dark:group-hover:text-red-300">
                                            <Gamepad2 className="h-8 w-8" />
                                        </div>
                                    </div>

                                    <div className="flex h-20 shrink-0 flex-col justify-center border-t border-zinc-200 bg-white px-4 dark:border-zinc-800 dark:bg-zinc-900/90">
                                        <h2 className="truncate text-base font-semibold text-zinc-900 transition-colors group-hover:text-red-500 dark:text-zinc-100 dark:group-hover:text-red-400">
                                            {productName}
                                        </h2>
                                        <div className="mt-1 flex items-center justify-between text-xs text-zinc-500 dark:text-zinc-400">
                                            <div className="truncate">
                                                <span className="text-base font-bold text-zinc-900 group-hover:text-red-500 dark:text-white dark:group-hover:text-red-400">
                                                    {(product.price / 100).toLocaleString("th-TH")}
                                                </span>{" "}
                                                {t("บาท", "THB")}
                                            </div>
                                            <span className="inline-flex items-center gap-0.5 font-medium text-red-500 opacity-0 transition-opacity group-hover:opacity-100 dark:text-red-400">
                                                <Zap className="h-3 w-3" /> {t("เติมเลย", "Top up")}
                                            </span>
                                        </div>
                                    </div>
                                </Link>
                            );
                        })}
                    </div>
                )}
            </div>
        </main>
    );
}