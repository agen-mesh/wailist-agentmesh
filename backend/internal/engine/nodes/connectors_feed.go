package nodes

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"

	"github.com/agentmesh/backend/internal/models"
)

// rssFeed is the subset of RSS 2.0 this node reads. Atom is not handled — a
// separate element tree — and is left for a follow-up rather than
// half-supported here.
type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			PubDate     string `xml:"pubDate"`
			Description string `xml:"description"`
			GUID        string `xml:"guid"`
		} `xml:"item"`
	} `xml:"channel"`
}

const rssDefaultLimit = 10

// fetchRSS reads an RSS 2.0 feed and returns its items as structured data.
// Unlike every other connector this returns a value rather than a sentinel —
// downstream nodes address it with {{ node.<id>.title }}.
func fetchRSS(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	feedURL := resolveTemplate(configVal(node, "rssURL", ""), rc)
	if feedURL == "" {
		return "rss_skipped_no_url", ErrActionSkipped
	}
	limit := rssDefaultLimit
	if raw := configVal(node, "rssLimit", ""); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("rss: `rssLimit` %q is not a positive number", raw)
		}
		limit = n
	}

	body, err := getRaw(ctx, feedURL, map[string]string{"Accept": "application/rss+xml, application/xml, text/xml"}, "RSS")
	if err != nil {
		return nil, err
	}

	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("rss: could not parse the feed: %w", err)
	}
	if feed.Channel.Title == "" && len(feed.Channel.Items) == 0 {
		return nil, errors.New("rss: the response is not an RSS 2.0 feed (no channel title or items found)")
	}

	items := make([]map[string]any, 0, limit)
	for i, it := range feed.Channel.Items {
		if i >= limit {
			break
		}
		items = append(items, map[string]any{
			"title":       it.Title,
			"link":        it.Link,
			"pubDate":     it.PubDate,
			"description": it.Description,
			"guid":        it.GUID,
		})
	}
	return map[string]any{
		"title": feed.Channel.Title,
		"count": len(items),
		"items": items,
	}, nil
}
