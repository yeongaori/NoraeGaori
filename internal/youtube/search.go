package youtube

import (
	"context"
	"fmt"
	"time"

	"noraegaori/pkg/logger"

	"github.com/ppalone/ytsearch"
)

type SearchResult = ytsearch.VideoInfo

func Search(guildID, query string, requesterName, requesterID string) (*Song, error) {
	if IsYouTubeURL(query) {

		return GetVideoInfo(guildID, query, requesterName, requesterID)
	}

	logger.Debugf("Searching YouTube for: %s", query)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, err := searchClient.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search YouTube: %w", err)
	}

	if len(response.Results) == 0 {
		return nil, fmt.Errorf("no results found for: %s", query)
	}

	video := response.Results[0]
	videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", video.VideoID)

	return GetVideoInfo(guildID, videoURL, requesterName, requesterID)
}

func SearchMultipleContext(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if searchClient == nil {
		return nil, fmt.Errorf("search client not initialized")
	}

	response, err := searchClient.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to search YouTube: %w", err)
	}

	if len(response.Results) == 0 {
		return nil, fmt.Errorf("no results found")
	}

	if limit > 0 && len(response.Results) > limit {
		return response.Results[:limit], nil
	}

	return response.Results, nil
}

func SearchMultiple(query string, limit int) ([]SearchResult, error) {
	logger.Debugf("Searching YouTube for multiple results: %s", query)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return SearchMultipleContext(ctx, query, limit)
}
