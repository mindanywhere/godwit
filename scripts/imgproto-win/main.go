package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

var start = time.Now()

func ts() string { return fmt.Sprintf("%5dms", time.Since(start).Milliseconds()) }

func pngBytes() []byte {
	im := image.NewRGBA(image.Rect(0, 0, 60, 40))
	for x := 0; x < 60; x++ {
		for y := 0; y < 40; y++ {
			im.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 6), 128, 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, im)
	return b.Bytes()
}

var img = pngBytes()

// page loads its images the reader's way, at top level and inside a sandboxed
// srcdoc iframe with the reader's CSP, as the body pages do. The script
// reports every image's outcome through probe://app/log.
const page = `<!doctype html><html><head><meta charset="utf-8">
<style>body{margin:0;font:12px monospace}img{display:block;width:60px;height:40px;outline:1px solid red}</style></head>
<body>
<img id="a" src="http://m12.godwit-img.localhost/a">
<img id="b" src="http://m12.godwit-img.localhost/b">
<div style="width:60px;height:40px;background-image:url(http://m12.godwit-img.localhost/c)" id="c"></div>
<iframe id="f" sandbox="allow-same-origin allow-scripts" style="width:200px;height:140px"></iframe>
<script>
const log=(k,v)=>fetch('probe://app/log?k='+encodeURIComponent(k)+'&v='+encodeURIComponent(v)).catch(()=>{});
const FRAME = '<!doctype html><html><head><meta charset="utf-8">'
 + '<meta http-equiv="Content-Security-Policy" content="default-src \'none\'; style-src \'unsafe-inline\'; img-src data: godwit-img: http://*.godwit-img.localhost">'
 + '<style>img{display:block;width:60px;height:40px;outline:1px solid blue}</style></head><body>'
 + '<img id="d" src="http://m12.godwit-img.localhost/d">'
 + '<img id="e" src="http://m12.godwit-img.localhost/e">'
 + '</body></html>';
document.getElementById('f').srcdoc = FRAME;
function stateIn(doc,id){
  const im=doc.getElementById(id);
  if(!im) return 'none';
  if(im.complete&&im.naturalWidth>0) return 'loaded';
  if(im.complete) return 'failed';
  return 'pending';
}
window.addEventListener('load',()=>{
  const top=document;
  setTimeout(()=>{
    let fs='nodoc';
    try{const d=document.getElementById('f').contentDocument; fs=d?('d='+stateIn(d,'d')+' e='+stateIn(d,'e')):'nodoc'}catch(e){fs='err'}
    const s='a='+stateIn(top,'a')+' b='+stateIn(top,'b')+' frame='+fs;
    log('later',s);
    document.title='RESULT '+s;
  },1500);
});
</script>
</body></html>`

func main() {
	if err := mygo.Protocol.HandleFunc("godwit-img", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s godwit-img %s", ts(), r.URL.Path)
		w.Header().Set("Content-Type", "image/png")
		w.Write(img)
	}); err != nil {
		log.Fatal(err)
	}
	if err := mygo.Protocol.HandleFunc("probe", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/app/log"):
			log.Printf("%s LOG %s %s", ts(), r.URL.Query().Get("k"), r.URL.Query().Get("v"))
			w.WriteHeader(http.StatusNoContent)
		default:
			log.Printf("%s page %s", ts(), r.URL.Path)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(page))
		}
	}); err != nil {
		log.Fatal(err)
	}
	mygo.App.SetName("imgproto")
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{Title: "imgproto", Width: 300, Height: 260})
		win.Show()
		win.Page().LoadURL("probe://localhost/")
		log.Printf("%s window up", ts())
		go func() {
			for range 30 {
				time.Sleep(500 * time.Millisecond)
				if t := win.Title(); strings.Contains(t, "RESULT") {
					log.Printf("%s %s", ts(), t)
					mygo.App.Quit()
					return
				}
			}
			log.Printf("%s no result; title %q", ts(), win.Title())
			mygo.App.Quit()
		}()
	})
	mygo.App.Run()
}
