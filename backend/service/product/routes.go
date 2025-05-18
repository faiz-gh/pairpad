package product

import (
	"net/http"

	"github.com/faiz-gh/go-postgresql-starter/service/auth"
	"github.com/faiz-gh/go-postgresql-starter/types"
	"github.com/faiz-gh/go-postgresql-starter/utils"
	"github.com/gorilla/mux"
)

type Handler struct {
	store     types.ProductStore
	userStore types.UserStore
}

func NewHandler(store types.ProductStore, userStore types.UserStore) *Handler {
	return &Handler{store: store, userStore: userStore}
}

func (h *Handler) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/products", auth.WithJWTAuth(h.HandleGetProducts, h.userStore)).Methods(http.MethodGet)
}

func (h *Handler) HandleGetProducts(w http.ResponseWriter, r *http.Request) {
	ps, err := h.store.GetProducts()
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err)
		return
	}

	utils.WriteJSON(w, http.StatusOK, ps)
}
