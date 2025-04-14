package http

var errorTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.StatusCode}} {{.StatusText}}</title>
  <style>
    :root {
      --primary-color: #1f2937;
      --text-color: #1f2937;
      --secondary-text: #6b7280;
    }
    * {
      margin: 0;
      padding: 0;
      box-sizing: border-box;
    }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      color: var(--text-color);
      background-color: #f9fafb;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 1rem;
      text-align: center;
      line-height: 1.5;
    }
    .container {
      max-width: 400px;
      width: 100%;
    }
    .code {
      font-size: 5rem;
      font-weight: 600;
      color: var(--primary-color);
      margin-bottom: 0.5rem;
      line-height: 1;
    }
    .title {
      font-size: 1.5rem;
      font-weight: 600;
      margin-bottom: 1rem;
    }
    .message {
      color: var(--secondary-text);
      margin-bottom: 2rem;
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="code">{{.StatusCode}}</div>
    <div class="title">{{.StatusText}}</div>
    <p class="message">{{.StatusError}}</p>
  </div>
</body>
</html>
`

var dirTemplatePure = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Index of {{.DirName}}</title>
</head>
<body>
  <h3>Index of {{.DirName}}</h3><hr>
  <pre>{{range .Files}}{{if .IsDir}}
<a href="{{.Name}}/">{{.Name}}/</a>{{else}}
<a href="{{.Name}}">{{.Name}}</a>{{end}}{{end}}
  </pre>
</body>
</html>
`

var dirTemplatePretty = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Index of {{.DirName}}</title>
  <style>
    * {
      margin: 0;
      padding: 0;
      box-sizing: border-box;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    }
    body {
      background: #f8f9fa;
      color: #212529;
      line-height: 1.6;
      padding: 2rem 1rem;
      max-width: 800px;
      margin: 0 auto;
    }
    .header {
      border-bottom: 2px solid #e9ecef;
      padding-bottom: 1rem;
      margin-bottom: 1.5rem;
    }
    .title {
      font-size: 1.2rem;
      color: #2c3e50;
      font-weight: 600;
    }
    .list {
      background: white;
      border-radius: 8px;
      box-shadow: 0 2px 4px rgba(0, 0, 0, 0.05);
      overflow: hidden;
    }
    .item {
      display: flex;
      align-items: center;
      padding: 0.4rem 1rem;
      border-bottom: 1px solid #f1f3f5;
      transition: background 0.2s;
      text-decoration: none;
      color: #495057;
    }
    .item:hover {
      background: #f8f9fa;
    }
    .item:last-child {
      border-bottom: none;
    }
    .icon {
      width: 24px;
      color: #868e96;
    }
    .name {
      flex-grow: 1;
      font-size: 0.9rem;
    }
    .dir-icon::after {
      content: '📁';
    }
    .file-icon::after {
      content: '📄';
    }
    .meta {
      font-size: 0.85rem;
      color: #868e96;
      margin-left: 1rem;
    }
  </style>
</head>
<body>
  <div class="header">
    <div class="title">Index of {{.DirName}}</div>
  </div>
  <div class="list">{{range .Files}}{{if .IsDir}}
    <a href="{{.Name}}/" class="item">
      <div class="icon dir-icon"></div>
      <div class="name">{{.Name}}</div>
      <div class="meta">Directory</div>
    </a>{{else}}
    <a href="{{.Name}}" class="item">
      <div class="icon file-icon"></div>
      <div class="name">{{.Name}}</div>
      <div class="meta">File</div>
    </a>{{end}}{{end}}
  </div>
</body>
</html>
`
