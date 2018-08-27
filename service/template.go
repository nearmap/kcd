package kcd

const kcdListHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>kcd: Version list</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style id="intercom-stylesheet" type="text/css">
    body,h1{margin:0}body{padding:0}h1,p,table{font-size:14px;font-family:OpenSans,Lucida Grande,Lucida Sans Unicode,sans-serif;font-weight:100;width:100%;color:#555;padding:0px;border-collapse:collapse;margin:1px;}h1{font-size:18px;width:100%;border-bottom:solid 1px #eee;color:#7d7d7d;padding:3px}td,th{text-align:left;padding-bottom:3px}table{margin:15px}
    </style>
    <style type="text/css">
    p.progress{color:#000000;margin:0;font-weight:bold}tr.progress{background-color:#668cff;color:#000000;font-weight:bold}
    </style>
    <style type="text/css">
    p.failed{color:#000000;margin:0;font-weight:bold}tr.failed{background-color:#ff3333;color:#000000;font-weight:bold}
    </style>
    <style type="text/css">
    p.success{color:#000000;margin:0;font-weight:bold}tr.success{background-color:#66ff66;color:#000000;font-weight:bold}
    </style>
    {{if .Reload}}
    <script>
        window.onload = function() {
            setTimeout(function () {
                location.reload(true);
            }, 60000);
        };
    </script>
    {{end}}
</head>
<body>
<main>
    <h1>{{if eq .Namespace ""}}All Namespaces{{else}}{{.Namespace}}{{end}}</h1>
    <section>
        <div>
            <table >
                <thead>
                <tr>
                    {{if eq .Namespace ""}}<th scope="col">Namespace</th>{{end}}
                    <th scope="col">Name</th>
                    <th scope="col">Container</th>
                    <th scope="col">Status</th>
                    <th scope="col">Current Version</th>
                    <th scope="col">Live Version (if different)</th>
                </tr>
                </thead>
                <tbody>
                {{range .Resources}}
                <tr {{if eq .Status "Failed"}}class="failed"{{else if eq .Status "Progressing"}}class="progress"{{else if and (eq .Status "Success") .Recent}}class="success"{{end}} >
                    {{if eq $.Namespace ""}}<td>{{.Namespace}}</td>{{end}}
                    <td>{{.Name}}</td>
                    <td>{{.Container}}</td>
                    <td><p {{if eq .Status "Failed"}}class="failed"{{else if eq .Status "Progressing"}}class="progress"{{else if and (eq .Status "Success") .Recent}}class="success"{{end}}>{{.Status}}</td>
                    <td>{{.CurrVersion}}</td>
                    <td>{{if ne .LiveVersion .CurrVersion}}{{.LiveVersion}}{{end}}</td>
                </tr>
                {{end}}
                </tbody>
            </table>
        </div>
    </section>
</main>
</body>
</html>
`
