package httpapi

import (
	"html/template"
	"net/http"
	"strconv"
)

var uploadPageTemplate = template.Must(template.New("upload-page").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>finance-sys Upload</title>
  <style>
    :root {
      --bg: #f2efe8;
      --panel: #fffdf8;
      --line: #d7c9b7;
      --text: #1e1b18;
      --muted: #6b6258;
      --accent: #144d3b;
      --accent-2: #c56a2d;
      --danger: #a53f2b;
      --ok: #1f6a49;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
      color: var(--text);
      background:
        radial-gradient(circle at top left, rgba(197,106,45,0.18), transparent 28%),
        linear-gradient(180deg, #efe7da 0%, var(--bg) 42%, #ece7df 100%);
      min-height: 100vh;
    }
    .wrap {
      width: min(1040px, calc(100% - 32px));
      margin: 32px auto 48px;
    }
    .hero {
      display: grid;
      gap: 10px;
      margin-bottom: 20px;
      padding: 28px;
      border: 1px solid rgba(20,77,59,0.12);
      border-radius: 24px;
      background: linear-gradient(135deg, rgba(255,253,248,0.94), rgba(244,238,229,0.98));
      box-shadow: 0 20px 40px rgba(30,27,24,0.08);
    }
    .hero h1 {
      margin: 0;
      font-size: clamp(28px, 4vw, 44px);
      line-height: 1;
      letter-spacing: -0.04em;
    }
    .hero p {
      margin: 0;
      color: var(--muted);
      font-size: 15px;
    }
    .grid {
      display: grid;
      grid-template-columns: 1.1fr 0.9fr;
      gap: 20px;
    }
    .card {
      border: 1px solid var(--line);
      border-radius: 22px;
      background: var(--panel);
      box-shadow: 0 14px 30px rgba(30,27,24,0.06);
      padding: 22px;
    }
    .card h2 {
      margin: 0 0 14px;
      font-size: 20px;
    }
    .hint {
      margin: 0 0 14px;
      color: var(--muted);
      font-size: 13px;
    }
    .fields {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 12px;
      margin-bottom: 18px;
    }
    .field {
      display: grid;
      gap: 6px;
    }
    .field.full {
      grid-column: 1 / -1;
    }
    label {
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      color: var(--muted);
    }
    input[type="text"], input[type="file"] {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 14px;
      padding: 12px 14px;
      background: #fff;
      color: var(--text);
      font-size: 14px;
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      margin-top: 14px;
    }
    button {
      border: none;
      border-radius: 999px;
      padding: 12px 18px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
      transition: transform 120ms ease, opacity 120ms ease;
    }
    button:hover { transform: translateY(-1px); }
    button:disabled { opacity: 0.55; cursor: wait; transform: none; }
    .primary { background: var(--accent); color: #f7f4ef; }
    .secondary { background: #efe3d6; color: var(--text); }
    .danger { background: var(--accent-2); color: #fff8f2; }
    .status {
      margin-top: 14px;
      padding: 12px 14px;
      border-radius: 14px;
      background: #f5efe6;
      color: var(--muted);
      min-height: 44px;
      white-space: pre-wrap;
    }
    .status.ok { background: rgba(31,106,73,0.1); color: var(--ok); }
    .status.error { background: rgba(165,63,43,0.1); color: var(--danger); }
    .results {
      display: grid;
      gap: 12px;
      max-height: 72vh;
      overflow: auto;
      padding-right: 4px;
    }
    .result {
      border: 1px solid rgba(20,77,59,0.12);
      border-radius: 16px;
      padding: 14px;
      background: #fffcf8;
    }
    .result strong {
      display: block;
      margin-bottom: 6px;
      font-size: 15px;
    }
    .meta {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-bottom: 10px;
    }
    .pill {
      border-radius: 999px;
      padding: 4px 10px;
      background: #efe7da;
      color: var(--muted);
      font-size: 12px;
    }
    pre {
      margin: 0;
      padding: 12px;
      border-radius: 14px;
      background: #1b1917;
      color: #f3ede5;
      font-size: 12px;
      overflow: auto;
    }
    @media (max-width: 820px) {
      .grid { grid-template-columns: 1fr; }
      .fields { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <div class="wrap">
    <section class="hero">
      <h1>Document Upload Console</h1>
      <p>本地页面，直接调用当前服务的上传接口。文件类型由上传文件后缀决定；作者和机构不在上传阶段填写，后续由分析层按文档内容抽取。</p>
    </section>

    <div class="grid">
      <section class="card">
        <h2>Upload</h2>
        <p class="hint">当前接口：<code>{{.APIPrefix}}/documents/upload</code></p>

        <div class="fields">
          <div class="field full">
            <label for="singleFile">Single File</label>
            <input id="singleFile" type="file" accept=".pdf,.doc,.docx,.txt,.md,.csv">
          </div>
          <div class="field full">
            <label for="batchFiles">Batch Files</label>
            <input id="batchFiles" type="file" accept=".pdf,.doc,.docx,.txt,.md,.csv" multiple>
          </div>
        </div>

        <div class="actions">
          <button id="singleBtn" class="primary" type="button">上传单个文件</button>
          <button id="batchBtn" class="danger" type="button">批量上传全部</button>
          <button id="clearBtn" class="secondary" type="button">清空结果</button>
        </div>

        <div id="status" class="status">等待上传。</div>
      </section>

      <section class="card">
        <h2>Results</h2>
        <p class="hint">上传完成后显示接口返回，便于检查 document 和 plans。</p>
        <div id="results" class="results"></div>
      </section>
    </div>
  </div>

  <script>
    const apiPrefix = {{.APIPrefixLiteral}};
    const uploadURL = apiPrefix + "/documents/upload";
    const statusBox = document.getElementById("status");
    const resultsBox = document.getElementById("results");
    const singleBtn = document.getElementById("singleBtn");
    const batchBtn = document.getElementById("batchBtn");
    const clearBtn = document.getElementById("clearBtn");

    const singleInput = document.getElementById("singleFile");
    const batchInput = document.getElementById("batchFiles");

    function formValues(file) {
      return {
        title: file.name.replace(/\.[^.]+$/, "")
      };
    }

    function setBusy(busy) {
      singleBtn.disabled = busy;
      batchBtn.disabled = busy;
    }

    function setStatus(text, kind) {
      statusBox.textContent = text;
      statusBox.className = "status" + (kind ? " " + kind : "");
    }

    function addResult(fileName, ok, payload) {
      const card = document.createElement("article");
      card.className = "result";

      const title = document.createElement("strong");
      title.textContent = fileName;
      card.appendChild(title);

      const meta = document.createElement("div");
      meta.className = "meta";
      meta.innerHTML =
        '<span class="pill">' + (ok ? "success" : "failed") + '</span>' +
        '<span class="pill">' + new Date().toLocaleTimeString() + '</span>';
      card.appendChild(meta);

      const pre = document.createElement("pre");
      pre.textContent = JSON.stringify(payload, null, 2);
      card.appendChild(pre);

      resultsBox.prepend(card);
    }

    async function uploadFile(file) {
      const values = formValues(file);
      const body = new FormData();
      body.append("file", file);
      body.append("title", values.title);

      const response = await fetch(uploadURL, {
        method: "POST",
        body
      });

      let payload;
      try {
        payload = await response.json();
      } catch (_) {
        payload = { error: "response is not valid JSON" };
      }

      if (!response.ok) {
        throw payload;
      }
      return payload;
    }

    async function uploadSingle() {
      const file = singleInput.files[0];
      if (!file) {
        setStatus("请先选择一个单文件。", "error");
        return;
      }

      setBusy(true);
      setStatus("正在上传单个文件: " + file.name, "");
      try {
        const payload = await uploadFile(file);
        addResult(file.name, true, payload);
        setStatus("单个上传完成: " + file.name, "ok");
      } catch (errorPayload) {
        addResult(file.name, false, errorPayload);
        setStatus("单个上传失败: " + file.name, "error");
      } finally {
        setBusy(false);
      }
    }

    async function uploadBatch() {
      const files = Array.from(batchInput.files);
      if (files.length === 0) {
        setStatus("请先选择批量文件。", "error");
        return;
      }

      setBusy(true);
      let success = 0;
      for (let index = 0; index < files.length; index += 1) {
        const file = files[index];
        setStatus("批量上传中 " + (index + 1) + "/" + files.length + ": " + file.name, "");
        try {
          const payload = await uploadFile(file);
          addResult(file.name, true, payload);
          success += 1;
        } catch (errorPayload) {
          addResult(file.name, false, errorPayload);
        }
      }
      setStatus("批量上传完成，成功 " + success + " / " + files.length, success === files.length ? "ok" : "error");
      setBusy(false);
    }

    singleBtn.addEventListener("click", uploadSingle);
    batchBtn.addEventListener("click", uploadBatch);
    clearBtn.addEventListener("click", () => {
      resultsBox.innerHTML = "";
      setStatus("结果已清空。", "");
    });
  </script>
</body>
</html>`))

type uploadPageData struct {
	APIPrefix        string
	APIPrefixLiteral template.JS
}

func (s *Server) handleUploadPage(w http.ResponseWriter, r *http.Request) {
	cfg := s.runtime.Config()
	apiPrefix := "/api/v1"
	if cfg != nil && cfg.Service.HTTP.APIPrefix != "" {
		apiPrefix = cfg.Service.HTTP.APIPrefix
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = uploadPageTemplate.Execute(w, uploadPageData{
		APIPrefix:        apiPrefix,
		APIPrefixLiteral: template.JS(strconv.Quote(apiPrefix)),
	})
}
