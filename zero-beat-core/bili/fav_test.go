package bili

import "testing"

func TestFavoriteDealForm(t *testing.T) {
	form := favoriteDealForm(12345, 67890, "csrf-token")
	for key, want := range map[string]string{
		"rid":           "12345",
		"type":          "2",
		"add_media_ids": "67890",
		"del_media_ids": "",
		"csrf":          "csrf-token",
		"platform":      "web",
	} {
		if got := form.Get(key); got != want {
			t.Fatalf("%s = %q，期望 %q", key, got, want)
		}
	}
}
