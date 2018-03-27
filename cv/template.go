package cv

const cvListHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>CVManager: Version list</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
</head>
<body>
<main class="flex flex-column min-vh-100"><!-- use flex to separate the layouts into diff sections. -->
    <header class="w-100">
        <div class="w-30 dib bg-washed-blue">
            List of current version of CV managed deployments
        </div>
    </header>
    <section class="flex-auto flex flex-column flex-row-ns">
        <div class="pv4 pv2-ns ph2 overflow-y-scroll w-70-ns">
            <blockquote class="lh-copy">
                <table >
                    <!-- Table header -->
                    <thead>
                    <tr>
                        <th scope="col">Namespace</th>
                        <th scope="col">Deployment</th>
                        <th scope="col">Container</th>
                        <th scope="col">Version</th>
                        <th scope="col">Status</th>
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
					    <td>{{.Status}}</td>
                    </tr>
                    {{end}}
                    </tbody>
                </table>
            </blockquote>
        </div>
    </section>
    <footer class="bg-light-green pa4 tc">
        <span class="f6">CVManager</span>
    </footer>
</main>
</body>
</html>

`
