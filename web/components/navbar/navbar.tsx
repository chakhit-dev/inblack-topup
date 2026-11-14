"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Search, ShieldCheck, Globe, Menu, X, Sun, Moon } from "lucide-react";
import { useLanguage } from "../button/context";
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useTheme } from "next-themes";
import { Button } from "../ui/button";

export default function Navbar() {
    const pathname = usePathname();
    const { lang, toggleLang, t } = useLanguage();
    const [adminRole, setAdminRole] = useState<string | null>(null);
    const [mobileOpen, setMobileOpen] = useState(false);
    const { setTheme } = useTheme();

    useEffect(() => {
        const checkAdmin = () => {
            try {
                const raw = localStorage.getItem("inblack_admin_user");
                if (raw) {
                    const parsed = JSON.parse(raw);
                    setAdminRole(parsed.role || null);
                } else {
                    setAdminRole(null);
                }
            } catch {
                setAdminRole(null);
            }
        };

        checkAdmin();
        window.addEventListener("storage", checkAdmin);
        return () => window.removeEventListener("storage", checkAdmin);
    }, [pathname]);

    const navLinkClass = (path: string) =>
        `flex items-center gap-1.5 rounded-lg px-3 py-2 text-sm font-medium transition ${
            pathname === path
                ? "bg-red-600/15 text-red-600 dark:text-red-500"
                : "text-zinc-600 hover:bg-red-600/10 hover:text-red-600 dark:text-zinc-400 dark:hover:bg-red-600/15 dark:hover:text-red-500"
        }`;

    return (
        <header className="sticky top-0 z-50 border-b border-zinc-200/80 bg-white/80 backdrop-blur-md dark:border-zinc-800 dark:bg-neutral-900/80">
            <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-4">
                <div className="flex items-center gap-10">
                    <Link href="/" className="flex items-center gap-2.5 text-lg font-bold tracking-tight text-zinc-900 dark:text-white">
                        <span>
                            INBLACK <span className="text-red-500 dark:text-red-400">TOP-UP</span>
                        </span>
                    </Link>

                    <nav className="hidden items-center gap-2 md:flex">
                        <Link href="/" className={navLinkClass("/")}>
                            {t("เติมเกม", "Topup")}
                        </Link>

                        <Link href="/track" className={navLinkClass("/track")}>
                            <span>{t("เช็กสถานะออเดอร์", "Track Order")}</span>
                        </Link>
                    </nav>
                </div>

                <div className="hidden items-center gap-2.5 md:flex">
                    <button
                        type="button"
                        onClick={toggleLang}
                        title="Switch Language"
                        className="flex items-center gap-1.5 rounded-lg border border-zinc-200 px-3 py-2 text-xs font-semibold text-zinc-800 transition hover:border-zinc-300 hover:bg-zinc-200/70 dark:border-zinc-800 dark:bg-zinc-900/70 dark:text-zinc-200 dark:hover:border-zinc-700 dark:hover:text-white"
                    >
                        <Globe className="h-3.5 w-3.5 text-red-500 dark:text-red-400" />
                        <span>{lang.toUpperCase()}</span>
                    </button>

                    <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                            <Button variant="outline" size="icon">
                                <Sun className="h-[1.2rem] w-[1.2rem] scale-100 rotate-0 transition-all dark:scale-0 dark:-rotate-90" />
                                <Moon className="absolute h-[1.2rem] w-[1.2rem] scale-0 rotate-90 transition-all dark:scale-100 dark:rotate-0" />
                                <span className="sr-only">Toggle theme</span>
                            </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={() => setTheme("light")}>
                                Light
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => setTheme("dark")}>
                                Dark
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => setTheme("system")}>
                                System
                            </DropdownMenuItem>
                        </DropdownMenuContent>
                    </DropdownMenu>
                </div>

                <div className="flex items-center gap-2 md:hidden">
                    <button
                        type="button"
                        onClick={toggleLang}
                        className="flex items-center gap-1 rounded-lg border border-zinc-200 bg-zinc-100 px-2.5 py-1.5 text-xs font-semibold text-zinc-800 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-200"
                    >
                        <Globe className="h-3.5 w-3.5 text-red-500 dark:text-red-400" />
                        {lang.toUpperCase()}
                    </button>

                    <button
                        type="button"
                        onClick={() => setMobileOpen(!mobileOpen)}
                        className="rounded-lg border border-zinc-200 bg-zinc-100 p-2 text-zinc-700 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300"
                    >
                        {mobileOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
                    </button>
                </div>
            </div>

            {mobileOpen && (
                <div className="space-y-1 border-t border-zinc-200 bg-white px-4 py-3 md:hidden dark:border-zinc-800 dark:bg-zinc-950">
                    <Link href="/" onClick={() => setMobileOpen(false)} className={navLinkClass("/")}>
                        {t("เติมเกม", "Top-Up")}
                    </Link>
                    <Link href="/track" onClick={() => setMobileOpen(false)} className={navLinkClass("/track")}>
                        <Search className="h-4 w-4 text-red-500 dark:text-red-400" />
                        <span>{t("เช็กสถานะออเดอร์", "Track Order")}</span>
                    </Link>
                    <Link href="/admin" onClick={() => setMobileOpen(false)} className={navLinkClass("/admin")}>
                        <ShieldCheck className="h-4 w-4 text-emerald-500 dark:text-emerald-400" />
                        <span>Backoffice {adminRole ? `(${adminRole})` : ""}</span>
                    </Link>
                </div>
            )}
        </header>
    );
}