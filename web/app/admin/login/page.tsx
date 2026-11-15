"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import {
  AlertCircle,
  ArrowLeft,
  CheckCircle2,
  KeyRound,
  Loader2,
  Mail,
  ShieldAlert,
} from "lucide-react";
import { useLanguage } from "@/components/button/context";
import { apiFetch } from "@/lib/api";

export default function AdminLoginPage() {
  const router = useRouter();
  const { t } = useLanguage();

  const [step, setStep] = useState<"request" | "verify">("request");
  const [email, setEmail] = useState("admin@inblackm.com");
  const [otp, setOtp] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [infoMsg, setInfoMsg] = useState<string | null>(null);

  useEffect(() => {
    try {
      const token = localStorage.getItem("inblack_admin_token");
      if (token) {
        router.replace("/admin");
      }
    } catch {
      // ignore storage error
    }
  }, [router]);

  async function handleRequestOtp(e: React.FormEvent) {
    e.preventDefault();
    const trimmedEmail = email.trim();
    if (!trimmedEmail) {
      setError(t("กรุณากรอกอีเมลผู้ดูแลระบบ", "Please enter admin email"));
      return;
    }

    setLoading(true);
    setError(null);
    setInfoMsg(null);

    try {
      await apiFetch("/auth/otp/request", {
        method: "POST",
        body: JSON.stringify({ email: trimmedEmail }),
      });

      setStep("verify");
      setInfoMsg(
        t(
          "ส่งรหัส OTP ไปยังอีเมลแล้ว (ตรวจสอบได้ใน Mailpit :8025)",
          "OTP sent to your email (check Mailpit at :8025)"
        )
      );
    } catch (err: unknown) {
      setError(
        err instanceof Error
          ? err.message
          : t("ไม่สามารถส่งรหัส OTP ได้", "Failed to send OTP")
      );
    } finally {
      setLoading(false);
    }
  }

  async function handleVerifyOtp(e: React.FormEvent) {
    e.preventDefault();
    const trimmedOtp = otp.trim();
    if (!trimmedOtp) {
      setError(t("กรุณากรอกรหัส OTP", "Please enter the OTP code"));
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const res = await apiFetch<{
        token?: string;
        access_token?: string;
        role?: string;
        user?: { id?: string; email: string; role: string };
        data?: {
          token?: string;
          access_token?: string;
          role?: string;
          user?: { id?: string; email: string; role: string };
        };
      }>("/auth/otp/verify", {
        method: "POST",
        body: JSON.stringify({
          email: email.trim(),
          otp: trimmedOtp,
          code: trimmedOtp,
        }),
      });

      const payload = res.data ? res.data : res;
      const token = payload.token || payload.access_token;

      if (!token) {
        throw new Error(
          t("ไม่ได้รับ Token ยืนยันตัวตนจากระบบ", "No authentication token returned")
        );
      }

      const userProfile = payload.user || {
        email: email.trim(),
        role: payload.role || "superadmin",
      };

      localStorage.setItem("inblack_admin_token", token);
      localStorage.setItem("inblack_admin_user", JSON.stringify(userProfile));
      window.dispatchEvent(new Event("storage"));

      router.push("/admin");
    } catch (err: unknown) {
      setError(
        err instanceof Error
          ? err.message
          : t("รหัส OTP ไม่ถูกต้องหรือหมดอายุ", "Invalid or expired OTP")
      );
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="mx-auto flex min-h-[calc(100vh-8rem)] max-w-md flex-col justify-center px-4 py-12">
      <div className="mb-6">
        <Link
          href="/"
          className="inline-flex items-center gap-1.5 text-sm font-medium text-zinc-600 transition hover:text-red-600 dark:text-zinc-400 dark:hover:text-red-400"
        >
          <ArrowLeft className="h-4 w-4" />
          {t("กลับหน้าร้านค้า", "Back to Storefront")}
        </Link>
      </div>

      <div className="rounded-2xl border border-zinc-200 bg-white p-7 shadow-lg sm:p-8 dark:border-zinc-800 dark:bg-zinc-900/80">
        <div className="mb-6 text-center">
          <div className="mx-auto mb-3 flex h-12 w-12 items-center justify-center rounded-2xl bg-red-500/10 text-red-600 ring-1 ring-red-500/20 dark:text-red-400">
            <ShieldAlert className="h-6 w-6" />
          </div>
          <h1 className="text-xl font-extrabold text-zinc-900 dark:text-white">
            {t("เข้าสู่ระบบผู้ดูแล (Backoffice)", "Admin Backoffice Login")}
          </h1>
          <p className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
            {t(
              "ยืนยันตัวตนด้วยรหัส OTP ผ่านอีเมล (superadmin / finance / support / viewer)",
              "Authenticate via Email OTP (superadmin / finance / support / viewer)"
            )}
          </p>
        </div>

        {infoMsg && (
          <div className="mb-4 flex items-start gap-2 rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-3 text-xs text-emerald-600 dark:text-emerald-400">
            <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
            <span>{infoMsg}</span>
          </div>
        )}

        {error && (
          <div className="mb-4 flex items-start gap-2 rounded-xl border border-red-500/30 bg-red-500/10 p-3 text-xs text-red-600 dark:text-red-400">
            <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        {step === "request" ? (
          <form onSubmit={handleRequestOtp} className="space-y-4">
            <div>
              <label
                htmlFor="admin-email"
                className="mb-1.5 block text-xs font-semibold text-zinc-700 dark:text-zinc-300"
              >
                {t("อีเมลผู้ดูแลระบบ", "Admin Email")}
              </label>
              <div className="relative">
                <Mail className="pointer-events-none absolute top-1/2 left-3.5 h-4 w-4 -translate-y-1/2 text-zinc-400" />
                <input
                  id="admin-email"
                  type="email"
                  autoComplete="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="admin@inblackm.com"
                  className="w-full rounded-xl border border-zinc-300 bg-zinc-50 py-2.5 pr-4 pl-10 text-sm text-zinc-900 transition outline-none focus:border-red-500 focus:bg-white focus:ring-2 focus:ring-red-500/20 dark:border-zinc-700 dark:bg-zinc-950 dark:text-white dark:focus:border-red-500 dark:focus:bg-zinc-900"
                />
              </div>
            </div>

            <button
              type="submit"
              disabled={loading}
              className="flex w-full items-center justify-center gap-2 rounded-xl bg-red-600 px-5 py-3 text-sm font-bold text-white shadow-md shadow-red-600/20 transition hover:bg-red-500 disabled:opacity-50"
            >
              {loading && <Loader2 className="h-4 w-4 animate-spin" />}
              <span>{t("ขอรหัส OTP", "Request OTP")}</span>
            </button>
          </form>
        ) : (
          <form onSubmit={handleVerifyOtp} className="space-y-4">
            <div>
              <label
                htmlFor="admin-otp"
                className="mb-1.5 block text-xs font-semibold text-zinc-700 dark:text-zinc-300"
              >
                {t(`รหัส OTP ที่ส่งไปยัง ${email}`, `OTP sent to ${email}`)}
              </label>
              <div className="relative">
                <KeyRound className="pointer-events-none absolute top-1/2 left-3.5 h-4 w-4 -translate-y-1/2 text-zinc-400" />
                <input
                  id="admin-otp"
                  type="text"
                  inputMode="numeric"
                  value={otp}
                  onChange={(e) => setOtp(e.target.value)}
                  placeholder="123456"
                  className="w-full rounded-xl border border-zinc-300 bg-zinc-50 py-2.5 pr-4 pl-10 font-mono text-base tracking-widest text-zinc-900 transition outline-none focus:border-red-500 focus:bg-white focus:ring-2 focus:ring-red-500/20 dark:border-zinc-700 dark:bg-zinc-950 dark:text-white dark:focus:border-red-500 dark:focus:bg-zinc-900"
                />
              </div>
            </div>

            <button
              type="submit"
              disabled={loading}
              className="flex w-full items-center justify-center gap-2 rounded-xl bg-red-600 px-5 py-3 text-sm font-bold text-white shadow-md shadow-red-600/20 transition hover:bg-red-500 disabled:opacity-50"
            >
              {loading && <Loader2 className="h-4 w-4 animate-spin" />}
              <span>{t("ยืนยันรหัส OTP และเข้าสู่ระบบ", "Verify OTP & Sign In")}</span>
            </button>

            <button
              type="button"
              onClick={() => {
                setStep("request");
                setOtp("");
                setError(null);
                setInfoMsg(null);
              }}
              className="w-full text-center text-xs font-medium text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200"
            >
              {t("เปลี่ยนอีเมล / ขอรหัสใหม่", "Change email / Resend OTP")}
            </button>
          </form>
        )}
      </div>
    </main>
  );
}