package service

import "testing"

func TestInferExternalDocumentAuthor(t *testing.T) {
	tests := map[string]string{
		"g界孙悟空7.23国产链.pdf":     "g界孙悟空",
		"Jacky一路向北7.23红利股.pdf": "Jacky一路向北",
		"没有日期.pdf":             "",
	}
	for fileName, want := range tests {
		if got := inferExternalDocumentAuthor(fileName); got != want {
			t.Fatalf("inferExternalDocumentAuthor(%q) = %q, want %q", fileName, got, want)
		}
	}
}
