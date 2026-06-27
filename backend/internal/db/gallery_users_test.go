package db

import "testing"

func TestNormalizeGalleryUserListOptions(t *testing.T) {
	tests := []struct {
		name   string
		input  GalleryUserListOptions
		expect GalleryUserListOptions
	}{
		{
			name:   "defaults limit and trims query",
			input:  GalleryUserListOptions{Query: "  demo  "},
			expect: GalleryUserListOptions{Limit: 30, Page: 1, Query: "demo"},
		},
		{
			name:   "clamps oversized limit and negative offset",
			input:  GalleryUserListOptions{Limit: 500, Offset: -10, Query: " api "},
			expect: GalleryUserListOptions{Limit: 100, Page: 1, Query: "api"},
		},
		{
			name:   "keeps valid pagination",
			input:  GalleryUserListOptions{Limit: 20, Page: 3},
			expect: GalleryUserListOptions{Limit: 20, Offset: 40, Page: 3},
		},
		{
			name:   "derives page from offset",
			input:  GalleryUserListOptions{Limit: 20, Offset: 40},
			expect: GalleryUserListOptions{Limit: 20, Offset: 40, Page: 3},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normalizeGalleryUserListOptions(test.input)
			if got != test.expect {
				t.Fatalf("unexpected options: got %#v want %#v", got, test.expect)
			}
		})
	}
}
