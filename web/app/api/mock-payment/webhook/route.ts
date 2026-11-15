import { NextRequest, NextResponse } from "next/server";
import crypto from "crypto";
import fs from "fs";
import path from "path";

function getRootEnvVar(key: string): string | undefined {
    if (process.env[key]) return process.env[key];

    try {
        const rootEnvPath = path.resolve(process.cwd(), "..", ".env");
        if (!fs.existsSync(rootEnvPath)) return undefined;

        const content = fs.readFileSync(rootEnvPath, "utf8");
        for (const line of content.split(/\r?\n/)) {
            const trimmed = line.trim();
            if (!trimmed || trimmed.startsWith("#")) continue;
            const eqIdx = trimmed.indexOf("=");
            if (eqIdx === -1) continue;
            const k = trimmed.slice(0, eqIdx).trim();
            if (k === key) {
                return trimmed.slice(eqIdx + 1).trim().replace(/^["']|["']$/g, "");
            }
        }
    } catch {
        return undefined;
    }
    return undefined;
}

export async function POST(req: NextRequest) {
    try {
        const rawBody = await req.text();
        const secret =
            getRootEnvVar("PAYMENT_WEBHOOK_SECRET") ||
            getRootEnvVar("WEBHOOK_SECRET");

        if (!secret) {
            return NextResponse.json(
                { detail: "PAYMENT_WEBHOOK_SECRET not found in root .env" },
                { status: 500 }
            );
        }

        const internalApiUrl =
            getRootEnvVar("INTERNAL_API_URL") || "http://localhost:8080";
        const timestamp = Math.floor(Date.now() / 1000).toString();

        const signature = crypto
            .createHmac("sha256", secret)
            .update(rawBody)
            .digest("hex");

        const res = await fetch(`${internalApiUrl}/api/v1/webhooks/payment`, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                "Idempotency-Key": req.headers.get("Idempotency-Key") || crypto.randomUUID(),
                "X-Signature": signature,
                "X-Webhook-Signature": signature,
                "X-Webhook-Timestamp": timestamp,
            },
            body: rawBody,
        });

        const data = await res.json().catch(() => ({}));
        return NextResponse.json(data, { status: res.status });
    } catch (err: unknown) {
        const message =
            err instanceof Error ? err.message : "Mock payment gateway callback failed";
        return NextResponse.json({ detail: message }, { status: 500 });
    }
}