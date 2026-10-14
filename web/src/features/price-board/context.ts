import { createContext } from "react";
import type { PriceStream } from "./types";

export const PriceStreamContext = createContext<PriceStream | null>(null);
