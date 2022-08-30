package nft

import (
	"bytes"
	"testing"
)

func TestContentFromCell(t *testing.T) {
	on := ContentSemichain{
		ContentOnchain: ContentOnchain{
			Name:        "Super",
			Description: "Puper",
			Image:       "/img.png",
			ImageData:   []byte{0xAA, 0xBB, 0xCC},
		},
		ContentOffchain: ContentOffchain{
			URI: "https://tonutils.com/22.json",
		},
	}
	err := on.SetAttribute("agility", "100500")
	if err != nil {
		t.Fatal(err)
	}

	c, err := on.ContentCell()
	if err != nil {
		t.Fatal(err)
	}

	content, err := ContentFromCell(c)
	if err != nil {
		t.Fatal(err)
	}

	on2 := content.(*ContentSemichain)
	if on2.URI != on.URI {
		t.Fatal("URI not eq:", on2.URI)
	}
	if on2.Image != on.Image {
		t.Fatal("Image not eq:", on2.Image)
	}
	if on2.Description != on.Description {
		t.Fatal("Description not eq:", on2.Description)
	}
	if on2.Name != on.Name {
		t.Fatal("Name not eq:", on2.Name)
	}
	if !bytes.Equal(on2.ImageData, on.ImageData) {
		t.Fatal("ImageData not eq:", on2.ImageData)
	}
	if on2.GetAttribute("agility") != "100500" {
		t.Fatal("Attr not eq:", on2.GetAttribute("agility"))
	}
}

func TestContentFromCell_Offchain(t *testing.T) {
	off := ContentOffchain{
		URI: "https://tonutils.com/22.json",
	}

	c, err := off.ContentCell()
	if err != nil {
		t.Fatal(err)
	}

	content, err := ContentFromCell(c)
	if err != nil {
		t.Fatal(err)
	}

	off2 := content.(*ContentOffchain)
	if off2.URI != off.URI {
		t.Fatal("URI not eq:", off2.URI)
	}
}
