/*
 * ComplyCo extension to Moov Watchman API
 */

package client

type SearchResult struct {
	IsSet     bool
	EntityID  *string  `json:"entityID,omitempty"`
	SdnName   *string  `json:"sdnName,omitempty"`
	Type      SdnType  `json:"type,omitempty"`
	Score     float64  `json:"score,omitempty"`
	Programs  []string `json:"programs,omitempty"`
	Remarks   string   `json:"remarks,omitempty"`
	Timestamp string   `json:"timestamp,omitempty"`
}
