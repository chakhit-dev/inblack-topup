export const API_BASE = process.env.NEXT_PUBLIC_API_URL || "/api/v1";

export interface Product {
    id: string;
    sku: string;
    slug: string;
    name_th: string;
    name_en: string;
    description_th?: string;
    description_en?: string;
    price: number; // satang
    stock: number | null;
    is_active: boolean;
}

export interface OrderItem {
    sku: string;
    product_name_th: string;
    product_name_en: string;
    unit_price: number;
    quantity: number;
    total_price: number;
}

export interface OrderEvent {
    id: number;
    from_status: string | null;
    to_status: string;
    actor: string;
    reason: string;
    created_at: string;
}

export interface Order {
    id: string;
    customer_email: string;
    game_uid: string;
    payment_method: string;
    voucher_code?: string | null;
    subtotal: number;
    discount: number;
    net: number;
    fee: number;
    total: number;
    vat: number;
    status: string;
    expires_at: string;
    created_at: string;
    items?: OrderItem[];
    events?: OrderEvent[];
}

export function formatTHB(satang: number): string {
    return new Intl.NumberFormat("th-TH", {
        style: "currency",
        currency: "THB",
        minimumFractionDigits: 2,
    }).format(satang / 100);
}

export function generateIdempotencyKey(prefix = "web"): string {
    if (typeof crypto !== "undefined" && crypto.randomUUID) {
        return `${prefix}-${crypto.randomUUID()}`;
    }
    return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

export async function apiFetch<T>(
    path: string,
    options: RequestInit = {}
): Promise<T> {
    const res = await fetch(`${API_BASE}${path}`, {
        ...options,
        headers: {
            "Content-Type": "application/json",
            ...(options.headers || {}),
        },
    });

    const data = await res.json().catch(() => ({}));
    if (!res.ok) {
        throw new Error(data.detail || data.title || data.message || `Request failed (${res.status})`);
    }
    return data as T;
}