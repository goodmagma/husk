// Command svg2png renders an SVG file to a square PNG, e.g. the program icon for packaging:
//
//	go run ./tools/svg2png assets/icon.svg assets/icon.png 256
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"strconv"

	"github.com/fyne-io/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: svg2png <input.svg> <output.png> <size>")
		os.Exit(2)
	}
	size, err := strconv.Atoi(os.Args[3])
	if err != nil || size < 1 {
		fmt.Fprintln(os.Stderr, "invalid size:", os.Args[3])
		os.Exit(2)
	}
	if err := convert(os.Args[1], os.Args[2], size); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func convert(in, out string, size int) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()
	icon, err := oksvg.ReadIconStream(f)
	if err != nil {
		return err
	}

	// oksvg does not scale stroke widths and corner radii reliably, so the icon is
	// rasterized at its viewBox size and then resampled to the requested size.
	w, h := int(icon.ViewBox.W), int(icon.ViewBox.H)
	icon.SetTarget(0, 0, float64(w), float64(h))
	native := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, native, native.Bounds())
	icon.Draw(rasterx.NewDasher(w, h, scanner), 1)

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(img, img.Bounds(), native, native.Bounds(), draw.Src, nil)

	o, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := png.Encode(o, img); err != nil {
		o.Close()
		return err
	}
	return o.Close()
}
