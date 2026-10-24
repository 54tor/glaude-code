// Monaco servi par gld lui-même (la CSP interdit tout CDN) : workers en fichiers locaux, langages choisis à la carte plutôt que le paquet complet.
import EditorWorker from "monaco-editor/editor/editor.worker.js?worker"
import TsWorker from "monaco-editor/languages/features/typescript/ts.worker.js?worker"
import JsonWorker from "monaco-editor/languages/features/json/json.worker.js?worker"
import CssWorker from "monaco-editor/languages/features/css/css.worker.js?worker"
import HtmlWorker from "monaco-editor/languages/features/html/html.worker.js?worker"
import "monaco-editor/features/register.all"
import "monaco-editor/languages/features/register.all"
import "monaco-editor/languages/definitions/typescript/register"
import "monaco-editor/languages/definitions/javascript/register"
import "monaco-editor/languages/definitions/css/register"
import "monaco-editor/languages/definitions/html/register"
import "monaco-editor/languages/definitions/markdown/register"
import "monaco-editor/languages/definitions/yaml/register"
import "monaco-editor/languages/definitions/shell/register"
import "monaco-editor/languages/definitions/go/register"
import "monaco-editor/languages/definitions/python/register"
import "monaco-editor/languages/definitions/php/register"
import "monaco-editor/languages/definitions/rust/register"
import "monaco-editor/languages/definitions/sql/register"
import "monaco-editor/languages/definitions/xml/register"
import "monaco-editor/languages/definitions/dockerfile/register"
import "monaco-editor/languages/definitions/ini/register"

self.MonacoEnvironment = {
  getWorker(_id: string, label: string) {
    if (label === "typescript" || label === "javascript") return new TsWorker()
    if (label === "json") return new JsonWorker()
    if (label === "css" || label === "scss" || label === "less") return new CssWorker()
    if (label === "html" || label === "handlebars" || label === "razor") return new HtmlWorker()
    return new EditorWorker()
  },
}
