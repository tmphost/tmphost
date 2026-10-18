package api

import (
	"github.com/gofiber/fiber/v3"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// Authenticate rejects requests without a valid session cookie and stores
// the user ID for the handlers behind it.
func Authenticate(controller authctrl.Controller) fiber.Handler {
	return func(c fiber.Ctx) error {
		rq := mapper.FromHTTPRequestToAuthenticateRequestEntity(c)
		if rq == nil {
			return sendError(c, fiber.StatusUnauthorized, "unauthorized")
		}
		rs, err := controller.Authenticate(c.Context(), rq)
		if err != nil {
			return sendError(c, fiber.StatusUnauthorized, "unauthorized")
		}
		c.Locals(mapper.LocalUserID, rs.UserID)
		return c.Next()
	}
}
