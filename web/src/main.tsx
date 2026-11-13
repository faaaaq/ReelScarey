import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createBrowserRouter } from "react-router-dom";
import "@radix-ui/themes/styles.css";
import "./styles/app.css";
import App from "./App";
import { ThemeProvider } from "./theme";

// A data router is required for useBlocker, which backs the unsaved-changes
// guard on the Settings General and Subtitle preference draft forms.
const router = createBrowserRouter([{ path: "*", element: <App /> }]);

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <RouterProvider router={router} />
    </ThemeProvider>
  </StrictMode>,
);
