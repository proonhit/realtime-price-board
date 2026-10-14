import { useContext } from "react";
import { PriceStreamContext } from "../context";
import type { PriceStream } from "../types";

export function usePriceStream(): PriceStream {
  const stream = useContext(PriceStreamContext);
  if (!stream) throw new Error("usePriceStream must be used inside <PriceStreamProvider>");
  return stream;
}
