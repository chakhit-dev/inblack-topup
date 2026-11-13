package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/inblack/topup-backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProductHandler struct {
	db *pgxpool.Pool
}

func NewProductHandler(db *pgxpool.Pool) *ProductHandler {
	return &ProductHandler{db: db}
}

type ProductResponse struct {
	ID            string    `json:"id"`
	SKU           string    `json:"sku"`
	Slug          string    `json:"slug"`
	NameTH        string    `json:"name_th"`
	NameEN        string    `json:"name_en"`
	Price         int64     `json:"price"`
	Stock         *int      `json:"stock"`
	LimitPerOrder *int      `json:"limit_per_order"`
	LimitPerEmail *int      `json:"limit_per_email"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (h *ProductHandler) ListProducts(c *gin.Context) {
	rows, err := h.db.Query(c.Request.Context(), `
		SELECT id, sku, slug, name_th, name_en, price, stock, limit_per_order, limit_per_email, created_at, updated_at
		FROM products
		ORDER BY created_at ASC, sku ASC
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to fetch products",
		))
		return
	}
	defer rows.Close()

	products := make([]ProductResponse, 0)
	for rows.Next() {
		var p ProductResponse
		if err := rows.Scan(
			&p.ID, &p.SKU, &p.Slug, &p.NameTH, &p.NameEN,
			&p.Price, &p.Stock, &p.LimitPerOrder, &p.LimitPerEmail,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, domain.NewProblem(
				http.StatusInternalServerError, domain.CodeInternalServerError, "Scan Error", "Failed to read product record",
			))
			return
		}
		products = append(products, p)
	}

	c.JSON(http.StatusOK, gin.H{"data": products})
}

// GetProductBySlug GET /api/v1/products/:slug
func (h *ProductHandler) GetProductBySlug(c *gin.Context) {
	slug := c.Param("slug")

	var p ProductResponse
	err := h.db.QueryRow(c.Request.Context(), `
		SELECT id, sku, slug, name_th, name_en, price, stock, limit_per_order, limit_per_email, created_at, updated_at
		FROM products
		WHERE slug = $1
	`, slug).Scan(
		&p.ID, &p.SKU, &p.Slug, &p.NameTH, &p.NameEN,
		&p.Price, &p.Stock, &p.LimitPerOrder, &p.LimitPerEmail,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, domain.NewProblem(
				http.StatusNotFound, domain.CodeNotFound, "Product Not Found", "No product found with the given slug",
			))
			return
		}
		c.JSON(http.StatusInternalServerError, domain.NewProblem(
			http.StatusInternalServerError, domain.CodeInternalServerError, "Database Error", "Failed to fetch product",
		))
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": p})
}
