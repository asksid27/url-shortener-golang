package routes

import (
	"os"
	"strconv"
	"time"

	"github.com/asksid27/url-shortener-golang/database"
	"github.com/asksid27/url-shortener-golang/helpers"

	"github.com/asaskevich/govalidator"
	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type request struct{
	URL string `json:"url"`
	CustomShort string `json:"custom-short"`
	Expiry time.Duration `json:"expiry"`
}

type response struct{
	URL string	`json:"url"`
	CustomShort string `json:"custom-short"`
	Expiry string `json:"expiry"`
	XRateRemaining int `json:"rate-limit"`
	XRateLimitReset time.Duration `json:"rate-limit-reset"`
}

func ShortenURL(c *fiber.Ctx) error {
	body := new(request)

	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "can not parse JSON"})
	}

	r2 := database.CreateClient(1)
	defer r2.Close()

	v, err := r2.Get(database.Ctx, c.IP()).Result()
	if err == redis.Nil {
		_ = r2.Set(database.Ctx, c.IP(), os.Getenv("API_QUOTA"), 30 * time.Minute).Err()
	} else if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "can not connect to DB",
		})
	}
	val := 10
	var limit time.Duration
	if v != "" {
		val, _ = strconv.Atoi(v)
		limit, _ = r2.TTL(database.Ctx, c.IP()).Result()
		if val < 1 {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "rate limit exceeded",
				"rate_limit_exceeded": limit / time.Nanosecond / time.Minute,
			})
		}
	}

	if !govalidator.IsURL(body.URL) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid URL"})
	}

	if !helpers.RemoveDomainError(body.URL) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "domain error"})
	}

	body.URL = helpers.EnforceHTTP(body.URL)

	id := uuid.New().String()[:6]

	if body.CustomShort != "" {
		id = body.CustomShort
	}

	r := database.CreateClient(0)
	defer r.Close()

	v, err = r.Get(database.Ctx, id).Result()
	if err != nil && err != redis.Nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "can not connect to DB",
		})
	}

	if v != "" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "custom url is already in use",
		})
	}

	if body.Expiry == 0 {
		body.Expiry = 24
	}

	err = r.Set(database.Ctx, id, body.URL, body.Expiry * time.Hour).Err()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "can not connect to DB",
		})
	}

	resp := response{
		URL: body.URL,
		CustomShort: os.Getenv("DOMAIN") + "/" + id,
		Expiry: body.Expiry.String(),
		XRateRemaining: val - 1,
		XRateLimitReset: limit / time.Nanosecond / time.Minute,
	}

	r2.Decr(database.Ctx, c.IP())
	
	return c.Status(fiber.StatusOK).JSON(resp)
}