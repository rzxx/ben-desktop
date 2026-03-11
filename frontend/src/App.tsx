import { HashRouter } from "./app/router/HashRouter";
import { WindowShell } from "./app/shell/WindowShell";

function App() {
  return (
    <HashRouter>
      <WindowShell />
    </HashRouter>
  );
}

export default App;
