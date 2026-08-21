package rest

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

// An uploaded file is where the documentation says it is.
//
// The REST page describes `input.files.<field>` with a filename, a content
// type, a size and base64 data — and nothing published that key. Files were
// only ever put flat on the input, so every transform written from the page
// failed with "no such key: files", which is not a hint that the shape is
// different.
func TestAnUploadedFileIsUnderFiles(t *testing.T) {
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	if err := form.WriteField("caption", "my dog"); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("avatar", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("not really a jpeg")
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("POST", "/users/42/avatar", body)
	r.Header.Set("Content-Type", form.FormDataContentType())

	input := map[string]interface{}{}
	(&Connector{}).parseMultipart(r, input)

	files, ok := input["files"].(map[string]interface{})
	if !ok {
		t.Fatalf("input.files = %#v, want the uploads", input["files"])
	}
	avatar, ok := files["avatar"].(map[string]interface{})
	if !ok {
		t.Fatalf("input.files.avatar = %#v", files["avatar"])
	}

	if avatar["filename"] != "photo.jpg" {
		t.Errorf("filename = %#v", avatar["filename"])
	}
	if avatar["size"] != int64(len(content)) {
		t.Errorf("size = %#v, want %d", avatar["size"], len(content))
	}
	decoded, err := base64.StdEncoding.DecodeString(avatar["data"].(string))
	if err != nil || string(decoded) != string(content) {
		t.Errorf("data did not come back as base64 of the file: %v", err)
	}

	// The ordinary form fields are flat, as they have always been.
	if input["caption"] != "my dog" {
		t.Errorf("caption = %#v", input["caption"])
	}
	// And the flat key stays, for configurations written against it.
	if _, present := input["avatar"]; !present {
		t.Error("the flat key was dropped, which would break configurations that use it")
	}
}

// A request that is not multipart leaves no files behind.
func TestNoFilesKeyWithoutAnUpload(t *testing.T) {
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	if err := form.WriteField("caption", "no file here"); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("POST", "/x", body)
	r.Header.Set("Content-Type", form.FormDataContentType())

	input := map[string]interface{}{}
	(&Connector{}).parseMultipart(r, input)

	if _, present := input["files"]; present {
		t.Errorf("input.files = %#v on a form with no files", input["files"])
	}
}
