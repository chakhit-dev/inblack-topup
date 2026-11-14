"use client";

import React, { createContext, useContext, useEffect, useState } from "react";

export type Lang = "th" | "en";

interface LanguageContextType {
  lang: Lang;
  setLang: (lang: Lang) => void;
  toggleLang: () => void;
  t: (th: string, en: string) => string;
}

const LanguageContext = createContext<LanguageContextType>({
  lang: "th",
  setLang: () => { },
  toggleLang: () => { },
  t: (th) => th,
});

export function LanguageProvider({ children }: { children: React.ReactNode }) {
  const [lang, setLangState] = useState<Lang>("th");
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
    const saved = (localStorage.getItem("inblack_lang") || localStorage.getItem("lang")) as Lang | null;
    if (saved === "th" || saved === "en") {
      setLangState(saved);
    }
  }, []);

  function setLang(newLang: Lang) {
    setLangState(newLang);
    localStorage.setItem("inblack_lang", newLang);
    localStorage.setItem("lang", newLang);
  }

  function toggleLang() {
    setLang(lang === "th" ? "en" : "th");
  }

  const activeLang: Lang = mounted ? lang : "th";

  function t(th: string, en: string): string {
    return activeLang === "th" ? th : en;
  }

  return (
    <LanguageContext.Provider value={{ lang: activeLang, setLang, toggleLang, t }}>
      {children}
    </LanguageContext.Provider>
  );
}

export function useLanguage() {
  return useContext(LanguageContext);
}