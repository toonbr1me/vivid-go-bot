package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"

	"github.com/toonbr1me/vivid-go-bot/internal/appearance"
	"github.com/toonbr1me/vivid-go-bot/internal/bot"
)

// ExportScreenLab writes a self-contained, offline fragment. It contains only
// fixtures and the same rendering resources used by the protected web gallery.
func ExportScreenLab(w io.Writer) error {
	resources, err := bot.PreviewAssets(appearance.Default())
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		Screens []bot.PreviewScreen `json:"screens"`
		Markup  string              `json:"markup"`
		CSS     string              `json:"css"`
	}{bot.ScreenCatalog(), resources.Markup, resources.CSS})
	if err != nil {
		return err
	}
	source, err := assets.ReadFile("templates/screen_lab.html")
	if err != nil {
		return err
	}
	t, err := template.New("lab").Parse(string(source))
	if err != nil {
		return err
	}
	var markup bytes.Buffer
	if err = t.ExecuteTemplate(&markup, "screen-lab", pageData{Site: theme{Brand: "Vivid"}, Data: experienceData{Catalog: template.JS(payload)}}); err != nil {
		return err
	}
	css, err := assets.ReadFile("static/screen-lab.css")
	if err != nil {
		return err
	}
	js, err := assets.ReadFile("static/screen-lab.js")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "<!doctype html><html lang=\"ru\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Vivid · обзор карточек</title></head><body>%s\n<style>%s</style>\n<script>%s</script>\n<script>%s</script>\n", markup.String(), css, resources.Runtime, js)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, `<script>
(function(){var root=document.getElementById('vivid-screenlab'),mount=root.querySelector('[data-lab-mount]');mount.setAttribute('aria-label','Оформление карточки бота');if(!globalThis.Tweak)return;var tokens={radius:8,spacing:16,button_height:44,heading:'balanced',navigation:'bottom',accent:'#207ba6',telegram:true};var tweak=new Tweak({container:mount,onChange:function(){root.dispatchEvent(new CustomEvent('vivid:theme',{detail:{radius:tokens.radius,spacing:tokens.spacing,button_height:tokens.button_height,heading:tokens.heading,navigation:tokens.navigation,accent:tokens.telegram?'telegram':tokens.accent}}))}});tweak.addSlider(tokens,'radius',{label:'Скругление',min:4,max:20,step:2,unit:'px'});tweak.addSlider(tokens,'spacing',{label:'Отступы',min:8,max:20,step:4,unit:'px'});tweak.addSlider(tokens,'button_height',{label:'Высота кнопок',min:40,max:48,step:4,unit:'px'});tweak.addSelect(tokens,'heading',{label:'Заголовки',options:[{label:'Выразительные',value:'balanced'},{label:'Компактные',value:'compact'}]});tweak.addSelect(tokens,'navigation',{label:'Навигация',options:[{label:'Внизу',value:'bottom'},{label:'Сверху',value:'top'}]});tweak.addToggle(tokens,'telegram',{label:'Акцент Telegram'});tweak.addColorPicker(tokens,'accent',{label:'Свой акцент'});})();
</script>`)
	return err
}
