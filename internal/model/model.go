package model

type Device map[string]any

// ContentType is retained for compatibility with the shared worker model.
type ContentType int

const (
	ContentTypeUnknown ContentType = iota
	ContentTypeUGCVideo
	ContentTypePGCVideo
	ContentTypeUGCArticle
	ContentTypePGCArticle
)

type Target struct {
	ObjectID    string      `json:"object_id"`
	Vid         string      `json:"vid"`
	SeriesID    string      `json:"series_id"`
	ItemID      string      `json:"item_id"`
	ContentType ContentType `json:"content_type,omitempty"`
}

type ExternalAPIData struct {
	Parameter string     `json:"parameter"`
	BuyNumber int        `json:"buy_number"`
	BuyParams []BuyParam `json:"buy_params"`
}

type BuyParam struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Task struct {
	ID                int             `json:"id"`
	TaskID            string          `json:"task_id"`
	PostID            string          `json:"postid"`
	TotalQuantity     int             `json:"total_quantity"`
	CompletedQuantity int             `json:"completed_quantity"`
	ExternalAPIData   ExternalAPIData `json:"external_api_data"`
}
