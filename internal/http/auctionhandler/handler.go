package auctionhandler

import (
	"auctionbidgo/internal/services/auction"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc auction.IAuctionService
}

func New(svc auction.IAuctionService) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(r gin.IRoutes) {
	r.GET("/auctions", h.list)
	r.GET("/auctions/:id", h.info)
	r.POST("/auctions/:id/start", h.start)
	r.POST("/auctions/:id/stop", h.stop)
	r.POST("/auctions/:id/bid", h.bid)
}

// @Summary		Get auction details
// @Description	Returns full information about a single auction.
// @Tags			Auctions
// @Param			id	path		string	true	"Auction ID"	default(auc123)
// @Success		200	{object}	auction.AuctionDTO
// @Failure		404	{object}	ErrorResponse
// @Router			/auctions/{id} [get]
func (h *Handler) info(c *gin.Context) {
	dto, err := h.svc.GetAuction(c, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, dto)
}

// @Summary		List auctions
// @Description	Retrieves a paginated list of auctions, optionally filtered by status.
// @Tags			Auctions
// @Param			status	query		string	false	"Status filter"			Enums(RUNNING,FINISHED)
// @Param			limit	query		int		false	"Max results (0‑100)"	minimum(0)	maximum(100)	default(10)
// @Param			offset	query		int		false	"Offset for pagination"	minimum(0)	default(0)
// @Success		200		{array}		auction.AuctionDTO
// @Failure		400		{object}	ErrorResponse
// @Failure		500		{object}	ErrorResponse
// @Router			/auctions [get]
func (h *Handler) list(c *gin.Context) {
	var q ListAuctionsQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	out, err := h.svc.ListAuctions(c, q.Status, q.Limit, q.Offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

// @Summary		Start an auction
// @Description	Seller starts a time‑boxed auction.
// @Tags			Auctions
// @Param			id		path	string				true	"Auction ID"	default(auc123)
// @Param			body	body	StartAuctionBody	true	"Ends‑at payload"
// @Success		202
// @Router			/auctions/{id}/start [post]
func (h *Handler) start(ginCtx *gin.Context) {
	var body StartAuctionBody
	if err := ginCtx.ShouldBindJSON(&body); err != nil {
		ginCtx.JSON(http.StatusBadRequest, &ErrorResponse{Error: err.Error()})
		return
	}
	auctionID := ginCtx.Param("id")

	endsAt := body.EndsAt.UTC()
	if endsAt.Before(time.Now().UTC()) {
		ginCtx.JSON(http.StatusBadRequest, &ErrorResponse{Error: "ends_at must be in the future"})
		return
	}

	if err := h.svc.StartAuction(ginCtx.Request.Context(), auctionID, body.SellerID, endsAt); err != nil {
		ginCtx.JSON(http.StatusConflict, &ErrorResponse{Error: err.Error()})
		return
	}
	ginCtx.Status(http.StatusAccepted)
}

// @Summary		Stop an auction
// @Description	Seller (or admin) stops an auction early.
// @Tags			Auctions
// @Param			id	path	string	true	"Auction ID"	default(auc123)
// @Success		202
// @Failure		409	{object}	ErrorResponse
// @Router			/auctions/{id}/stop [post]
func (h *Handler) stop(ginCtx *gin.Context) {
	auctionID := ginCtx.Param("id")

	if err := h.svc.StopAuction(ginCtx.Request.Context(), auctionID); err != nil {
		ginCtx.JSON(http.StatusConflict, &ErrorResponse{Error: err.Error()})
		return
	}
	ginCtx.Status(http.StatusAccepted)
}

// @Summary		Place a bid
// @Description	Bidder places a higher bid.
// @Tags			Auctions
// @Param			id		path	string			true	"Auction ID"	default(auc123)
// @Param			body	body	PlaceBidBody	true	"Bid payload"
// @Success		202
// @Failure		400	{object}	ErrorResponse
// @Failure		409	{object}	ErrorResponse
// @Router			/auctions/{id}/bid [post]
func (h *Handler) bid(ginCtx *gin.Context) {
	var body PlaceBidBody
	if err := ginCtx.ShouldBindJSON(&body); err != nil {
		ginCtx.JSON(http.StatusBadRequest, &ErrorResponse{Error: err.Error()})
		return
	}

	auctionID := ginCtx.Param("id")
	if err := h.svc.PlaceBid(ginCtx.Request.Context(),
		auctionID,
		body.BidderID,
		body.Amount,
	); err != nil {
		ginCtx.JSON(http.StatusConflict, &ErrorResponse{Error: err.Error()})
		return
	}
	ginCtx.Status(http.StatusAccepted)
}
