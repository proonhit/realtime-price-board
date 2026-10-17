import { createBrowserRouter } from "react-router";
import { RequireAuth } from "../features/auth/components/RequireAuth";
import { BoardPage } from "../pages/BoardPage";
import { LoginPage } from "../pages/LoginPage";
import { RegisterPage } from "../pages/RegisterPage";
import { SymbolPage } from "../pages/SymbolPage";
import { WatchlistPage } from "../pages/WatchlistPage";
import { RootLayout } from "./RootLayout";
import { RouteError } from "./RouteError";

export const router = createBrowserRouter([
  {
    path: "/",
    element: <RootLayout />,
    children: [
      {
        errorElement: <RouteError />,
        children: [
          { index: true, element: <BoardPage /> },
          { path: "symbol/:code", element: <SymbolPage /> },
          {
            path: "watchlist",
            element: (
              <RequireAuth>
                <WatchlistPage />
              </RequireAuth>
            ),
          },
          { path: "login", element: <LoginPage /> },
          { path: "register", element: <RegisterPage /> },
          { path: "*", element: <RouteError /> },
        ],
      },
    ],
  },
]);
