import { createRoot } from "react-dom/client";

import { App } from "./app/App";
import "./styles/app.css";
import "./styles/playground.css";

createRoot(document.getElementById("root")!).render(<App />);
