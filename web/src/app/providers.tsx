import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { AuthProvider } from "../features/auth/components/AuthProvider";
import { PriceStreamProvider } from "../features/price-board/components/PriceStreamProvider";

export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(
    () => new QueryClient({ defaultOptions: { queries: { staleTime: 2_000, retry: 1 } } }),
  );

  // AuthProvider nằm trong QueryClientProvider vì nó dùng useQuery/useQueryClient.
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <PriceStreamProvider>{children}</PriceStreamProvider>
      </AuthProvider>
    </QueryClientProvider>
  );
}
