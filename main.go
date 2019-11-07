package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
)

var (
	db *gorm.DB
	e  *echo.Echo
	h  *handlers

	httpClient        = http.DefaultClient
	createChatRoomURL = "https://gateway.chotot.org/v2/public/chat/room/create"
	sendChatMsgURL    = "https://gateway.chotot.org/v2/public/chat/message/send"
	dbFmt             = "user=postgres dbname=bidDB host=127.0.0.1 port=13000 sslmode=disable"
)

type handlers struct {
	db *gorm.DB
}

// Bid schema
type Bid struct {
	CreatedAt time.Time `json:"-"`
	ListID    int       `json:"list_id" gorm:"index:idx_bid_list_id;unique"`
	Owner     int       `json:"owner" gorm:"index:owner"`
	TTL       int64     `json:"ttl"`
}

// Bidder stands for buyers bid
type Bidder struct {
	ListID     int    `json:"list_id" gorm:"primary_key;index:idx_bidder_list_id;unique"`
	Price      int    `json:"price" gorm:"index:price"`
	Bidder     int    `json:"bidder" gorm:"primary_key;index:bidder;unique"`
	Status     string `json:"status"`
	ChatRoomID string `json:"chat_room_id" gorm:"index:chat_room_id"`
}

// Room ...
type Room struct {
	Result struct {
		ID string `json:"_id"`
	} `json:"result"`
	Status string `json:"status"`
}

// BiddersResponse ...
type BiddersResponse struct {
	Count   int      `json:"count"`
	Bidders []Bidder `json:"bid"`
}

func init() {
	dbtmp, err := gorm.Open("postgres", dbFmt)
	if err != nil {
		log.Panicf("failed to connect database: %s", err.Error())
	}
	db = dbtmp

	// Migrate the schema
	dbtmp.AutoMigrate(&Bid{})
	dbtmp.AutoMigrate(&Bidder{})

	e = echo.New()
	h = &handlers{
		db: db,
	}

	e.POST("/bid", h.bid)
	e.GET("/bid/:list_id", h.getBid)

	e.POST("/bidder", h.bidder)
	e.PUT("/bidder", h.acceptBidder)
	e.GET("/bidder/:list_id", h.getBidders)
}

func (h *handlers) bid(c echo.Context) error {
	req := &Bid{}
	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	req.CreatedAt = time.Now()

	errs := h.db.Create(req).GetErrors()
	if errs != nil && len(errs) > 0 {
		return c.JSON(http.StatusBadRequest, errs)
	}

	return c.JSON(http.StatusOK, "OK")
}

func (h *handlers) getBid(c echo.Context) error {
	adID := c.Param("list_id")

	req := &Bid{}
	errs := h.db.Where("list_id = ?", adID).Find(req).GetErrors()
	if errs != nil && len(errs) > 0 {
		return c.JSON(http.StatusBadRequest, errs)
	}

	req.TTL = req.CreatedAt.Unix() + req.TTL - time.Now().Unix()

	return c.JSON(http.StatusOK, req)
}

func (h *handlers) bidder(c echo.Context) error {
	req := &Bidder{}
	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	token := c.Request().Header.Get(echo.HeaderAuthorization)
	if token != "" {
		token = token[7:]
	}

	room, err := sendBidderMsg(fmt.Sprintf("Bidding price %d", req.Price), token, *req)
	if err != nil {
		log.Println("sendBidderMsg error: ", err)
		return echo.NewHTTPError(http.StatusInternalServerError, err)
	}
	req.ChatRoomID = room.Result.ID

	errs := h.db.Create(req).GetErrors()
	if errs != nil && len(errs) > 0 {
		return c.JSON(http.StatusBadRequest, errs)
	}

	return c.JSON(http.StatusOK, "OK")
}

func sendBidderMsg(msg, token string, bidder Bidder) (*Room, error) {
	data := map[string]interface{}{
		"product": map[string]string{"item_id": fmt.Sprint(bidder.ListID)},
		"message": map[string]string{"text": msg},
	}
	log.Println("Data: ", data)

	req, err := newHTTPRequest(token, "POST", createChatRoomURL, data)
	if err != nil {
		return nil, err
	}

	res := Room{}
	_, err = httpClientDo(req, &res)
	if err != nil {
		return nil, err
	}
	log.Println(res)

	return &res, nil
}

// Should we add the token ?
func newHTTPRequest(token, method, path string, body interface{}) (*http.Request, error) {
	var buf io.ReadWriter
	if body != nil {
		buf = new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, path, buf)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Owner", "badboyd")

	return req, nil
}

func httpClientDo(req *http.Request, v interface{}) (*http.Response, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		io.Copy(ioutil.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode/100 != 2 {
		body, _ := ioutil.ReadAll(resp.Body)
		return resp, fmt.Errorf("%v", string(body))
	}

	if v != nil {
		d := json.NewDecoder(resp.Body)
		d.UseNumber()
		d.Decode(v)
	}

	return resp, err
}

func (h *handlers) acceptBidder(c echo.Context) error {
	req := &Bidder{}
	if err := c.Bind(req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	log.Println("accept bidder: ", req)

	errs := h.db.Model(req).Update("status", req.Status).GetErrors()
	if errs != nil && len(errs) > 0 {
		log.Println("Update error: ", errs)
		return c.JSON(http.StatusBadRequest, errs)
	}

	bid := Bid{}
	errs = h.db.Where("list_id = ?", req.ListID).Find(&bid).GetErrors()
	if errs != nil && len(errs) > 0 {
		return c.JSON(http.StatusBadRequest, errs)
	}

	token := c.Request().Header.Get(echo.HeaderAuthorization)
	if token != "" {
		token = token[7:]
	}
	sendMessage("Bid accepted", token, bid, *req)

	return c.JSON(http.StatusOK, "OK")
}

func sendMessage(message, token string, bid Bid, bidder Bidder) (*Room, error) {
	data := map[string]interface{}{
		"text":      message,
		"sender_id": bid.Owner,
		"room_id":   bidder.ChatRoomID,
		"unique_id": "85de4ffbb750b8a02420bb19481d3086",
		"draft_id":  "f872428baf8c69fdcf3c25b5ded68bf2",
	}

	req, err := newHTTPRequest(token, "POST", sendChatMsgURL, data)
	if err != nil {
		return nil, err
	}

	res := Room{}
	_, err = httpClientDo(req, &res)
	if err != nil {
		return nil, err
	}

	return &res, nil
}

func (h *handlers) getBidders(c echo.Context) error {
	adID := c.Param("list_id")

	bidders := []Bidder{}
	h.db.Where("list_id = ?", adID).Order("price").Limit(10).Find(&bidders)

	count := 0
	errs := h.db.Model(&Bidder{}).Where("list_id = ?", adID).Count(&count).GetErrors()
	if errs != nil && len(errs) > 0 {
		return c.JSON(http.StatusBadRequest, errs)
	}

	return c.JSON(http.StatusOK, BiddersResponse{Bidders: bidders, Count: count})
}

func main() {
	defer db.Close()

	if err := e.Start(":9000"); err != nil {
		log.Println("Cannot start the server")
	}
}
