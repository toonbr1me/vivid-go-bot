// Command screenlab exports the offline screen catalogue for design review.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/toonbr1me/vivid-go-bot/internal/web"
)

func main() {
	path := flag.String("export", "", "путь к HTML-каталогу")
	serve := flag.Bool("serve", false, "локальный пульт на изолированной демонстрационной базе")
	port := flag.Int("port", 8099, "порт демонстрационного пульта")
	themeDir := flag.String("theme", "web", "каталог темы для локального пульта")
	flag.Parse()
	if *path == "" && !*serve {
		fmt.Fprintln(os.Stderr, "укажите -export <путь.html>")
		os.Exit(2)
	}
	if *path != "" {
		if err := export(*path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if *serve {
		if err := serveLab(*port, *themeDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func export(path string) error {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	if err = web.ExportScreenLab(f); err != nil {
		_ = f.Close()
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	if err = f.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	fmt.Println("Каталог записан:", path)
	return nil
}
