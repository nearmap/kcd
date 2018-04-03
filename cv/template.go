package cv

const cvListHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>CVManager: Version list</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style id="intercom-stylesheet" type="text/css">
        body {
            padding: 0;
            margin: 0;
        }
        h1, p, table {
            font-size: 13px;
            font-family: OpenSans,Lucida Grande,Lucida Sans Unicode,sans-serif;
            font-weight: 100;
            width: 100%;
            color: #555;
        }
        h1 {
            font-size: 16px;
            width: 100%;
            border-bottom: solid 1px #eee;
            color: #7d7d7d;
            padding: 3px;
            margin: 0;
        }
        td, th {
            text-align: left;
            padding-bottom: 3px;
        }
        table {
            margin: 15px;
        }
    </style>
</head>
<body>
<main class="flex flex-column min-vh-100"><!-- use flex to separate the layouts into diff sections. -->
    <h1>List of current version of CV managed deployments</h1>
    <section class="flex-auto flex flex-column flex-row-ns">
        <div class="pv4 pv2-ns ph2 overflow-y-scroll w-70-ns">
            <table >
                <!-- Table header -->
                <thead>
                <tr>
                    <th scope="col">Namespace</th>
                    <th scope="col">Deployment</th>
                    <th scope="col">Container</th>
                    <th scope="col">Version</th>
                    <th scope="col">Available pods/Status</th>
                </tr>
                </thead>
                <!-- Table footer -->
                <tfoot>
                <tr>
                    <td>END</td>
                </tr>
                </tfoot>
                <!-- Table body -->
                <tbody>
                {{range .}}
                <tr>
				    <td>{{.Namespace}}</td>
				    <td>{{.Deployment}}</td>
				    <td>{{.Container}}</td>
				    <td>{{.Version}}</td>
				    <td>{{.AvailablePods}}</td>
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
