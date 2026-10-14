import { createBrowserRouter } from "react-router";
import { BoardPage } from "../pages/BoardPage";
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
          { path: "watchlist", element: <WatchlistPage /> },
          { path: "*", element: <RouteError /> },
        ],
      },
    ],
  },
]);
