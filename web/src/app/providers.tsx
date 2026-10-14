import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { PriceStreamProvider } from "../features/price-board/components/PriceStreamProvider";

export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 2_000, retry: 1 } } }),
  );

  return (
    <QueryClientProvider client={queryClient}>
      <PriceStreamProvider>{children}</PriceStreamProvider>
    </QueryClientProvider>
  );
}
