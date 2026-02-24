import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { motion, AnimatePresence } from 'framer-motion'
import { Menu, X, ChevronDown } from 'lucide-react'
import { t, type Language } from '../i18n/translations'
import { useSystemConfig } from '../hooks/useSystemConfig'

type Page =
  | 'competition'
  | 'traders'
  | 'trader'
  | 'backtest'
  | 'strategy'
  | 'debate'
  | 'login'
  | 'register'

interface HeaderBarProps {
  onLoginClick?: () => void
  isLoggedIn?: boolean
  isHomePage?: boolean
  currentPage?: Page
  language?: Language
  user?: { email: string } | null
  onLogout?: () => void
  onPageChange?: (page: Page) => void
  onLoginRequired?: (featureName: string) => void
}

export default function HeaderBar({
  isLoggedIn = false,
  isHomePage = false,
  currentPage,
  language = 'zh' as Language,
  user,
  onLogout,
  onPageChange,
  onLoginRequired,
}: HeaderBarProps) {
  const navigate = useNavigate()
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  const [userDropdownOpen, setUserDropdownOpen] = useState(false)
  const userDropdownRef = useRef<HTMLDivElement>(null)
  const { config: systemConfig } = useSystemConfig()
  const registrationEnabled = systemConfig?.registration_enabled !== false

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (
        userDropdownRef.current &&
        !userDropdownRef.current.contains(event.target as Node)
      ) {
        setUserDropdownOpen(false)
      }
    }

    document.addEventListener('mousedown', handleClickOutside)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [])

  return (
    <nav className="fixed top-0 w-full z-50 header-bar">
      <div className="flex items-center justify-between h-16 px-4 sm:px-6 max-w-[1920px] mx-auto">
        {/* Logo - Always go to home page */}
        <div
          onClick={() => {
            window.location.href = '/'
          }}
          className="flex items-center gap-2 hover:opacity-80 transition-opacity cursor-pointer"
        >
          <img src="/icons/nofx.svg" alt="Ares Logo" className="w-7 h-7" />
          <span className="text-lg font-bold text-nofx-gold">
            Ares
          </span>
        </div>

        {/* Desktop Menu */}
        <div className="hidden md:flex items-center justify-between flex-1 ml-8">
          {/* Left Side - Navigation Tabs - Always show all tabs */}
          <div className="flex items-center gap-2">
            {/* Navigation tabs configuration */}
            {(() => {
              // Define all navigation tabs
              const navTabs: { page: Page; path: string; label: string; requiresAuth: boolean }[] = [
                { page: 'traders', path: '/traders', label: t('configNav', language), requiresAuth: true },
                { page: 'trader', path: '/dashboard', label: t('dashboardNav', language), requiresAuth: true },
                { page: 'strategy', path: '/strategy', label: t('strategyNav', language), requiresAuth: true },
                { page: 'competition', path: '/competition', label: t('realtimeNav', language), requiresAuth: true },
                { page: 'debate', path: '/debate', label: t('debateNav', language), requiresAuth: true },
                { page: 'backtest', path: '/backtest', label: 'Backtest', requiresAuth: true },
              ]

              const handleNavClick = (tab: typeof navTabs[0]) => {
                // If requires auth and not logged in, show login prompt
                if (tab.requiresAuth && !isLoggedIn) {
                  onLoginRequired?.(tab.label)
                  return
                }
                // Navigate normally
                if (onPageChange) {
                  onPageChange(tab.page)
                }
                navigate(tab.path)
              }

              return navTabs.map((tab) => (
                <button
                  key={tab.page}
                  onClick={() => handleNavClick(tab)}
                  className={`text-sm font-bold transition-all duration-300 relative focus:outline-2 focus:outline-yellow-500 px-3 py-2 rounded-lg
                    ${currentPage === tab.page ? 'text-nofx-gold' : 'text-nofx-text-muted hover:text-nofx-gold'}`}
                >
                  {currentPage === tab.page && (
                    <span
                      className="absolute inset-0 rounded-lg bg-nofx-gold/15 -z-10"
                    />
                  )}
                  {tab.label}
                </button>
              ))
            })()}
          </div>

          {/* Right Side - User Actions */}
          <div className="flex items-center gap-4">

            {/* User Info and Actions */}
            {isLoggedIn && user ? (
              <div className="flex items-center gap-3">
                {/* User Info with Dropdown */}
                <div className="relative" ref={userDropdownRef}>
                  <button
                    onClick={() => setUserDropdownOpen(!userDropdownOpen)}
                    className="flex items-center gap-2 px-3 py-2 rounded transition-colors bg-nofx-bg-lighter border border-nofx-gold/20 hover:bg-white/5"
                  >
                    <div className="w-6 h-6 rounded-full flex items-center justify-center text-xs font-bold bg-nofx-gold text-black">
                      {user.email[0].toUpperCase()}
                    </div>
                    <span className="text-sm text-nofx-text-muted">
                      {user.email}
                    </span>
                    <ChevronDown className="w-4 h-4 text-nofx-text-muted" />
                  </button>

                  {userDropdownOpen && (
                    <div className="absolute right-0 top-full mt-2 w-48 rounded-lg shadow-lg overflow-hidden z-50 bg-nofx-bg-lighter border border-nofx-gold/20">
                      <div className="px-3 py-2 border-b border-nofx-gold/20">
                        <div className="text-xs text-nofx-text-muted">
                          {t('loggedInAs', language)}
                        </div>
                        <div className="text-sm font-medium text-nofx-text-muted">
                          {user.email}
                        </div>
                      </div>
                      {onLogout && (
                        <button
                          onClick={() => {
                            onLogout()
                            setUserDropdownOpen(false)
                          }}
                          className="w-full px-3 py-2 text-sm font-semibold transition-colors hover:opacity-80 text-center bg-nofx-danger/20 text-nofx-danger"
                        >
                          {t('exitLogin', language)}
                        </button>
                      )}
                    </div>
                  )}
                </div>
              </div>
            ) : (
              /* Show login/register buttons when not logged in and not on login/register pages */
              currentPage !== 'login' &&
              currentPage !== 'register' && (
                <div className="flex items-center gap-3">
                  <a
                    href="/login"
                    className="px-3 py-2 text-sm font-medium transition-colors rounded text-nofx-text-muted hover:text-white"
                  >
                    {t('signIn', language)}
                  </a>
                  {registrationEnabled && (
                    <a
                      href="/register"
                      className="px-4 py-2 rounded font-semibold text-sm transition-colors hover:opacity-90 bg-nofx-gold text-black"
                    >
                      {t('signUp', language)}
                    </a>
                  )}
                </div>
              )
            )}
          </div>
        </div>

        {/* Mobile Menu Button */}
        <motion.button
          onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
          className="md:hidden text-nofx-text-muted hover:text-white"
          whileTap={{ scale: 0.9 }}
        >
          {mobileMenuOpen ? (
            <X className="w-6 h-6" />
          ) : (
            <Menu className="w-6 h-6" />
          )}
        </motion.button>
      </div>

      {/* Mobile Menu Overlay */}
      <AnimatePresence>
        {mobileMenuOpen && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="fixed inset-0 z-40 md:hidden bg-black/90 backdrop-blur-xl"
            style={{ top: '64px' }} // Below header
          >
            <motion.div
              initial={{ y: -20, opacity: 0 }}
              animate={{ y: 0, opacity: 1 }}
              transition={{ delay: 0.1, duration: 0.3 }}
              className="flex flex-col h-[calc(100vh-64px)] overflow-y-auto px-6 py-8"
            >
              {/* Navigation Links */}
              <div className="flex flex-col gap-6 mb-12">
                {(() => {
                  const navTabs: { page: Page; path: string; label: string; requiresAuth: boolean }[] = [
                    { page: 'traders', path: '/traders', label: t('configNav', language), requiresAuth: true },
                    { page: 'trader', path: '/dashboard', label: t('dashboardNav', language), requiresAuth: true },
                    { page: 'strategy', path: '/strategy', label: t('strategyNav', language), requiresAuth: true },
                    { page: 'competition', path: '/competition', label: t('realtimeNav', language), requiresAuth: true },
                    { page: 'debate', path: '/debate', label: t('debateNav', language), requiresAuth: true },
                    { page: 'backtest', path: '/backtest', label: 'Backtest', requiresAuth: true },
                  ]

                  const handleMobileNavClick = (tab: typeof navTabs[0]) => {
                    if (tab.requiresAuth && !isLoggedIn) {
                      onLoginRequired?.(tab.label)
                      setMobileMenuOpen(false)
                      return
                    }
                    if (onPageChange) {
                      onPageChange(tab.page)
                    }
                    navigate(tab.path)
                    setMobileMenuOpen(false)
                  }

                  return navTabs.map((tab, i) => (
                    <motion.button
                      key={tab.page}
                      initial={{ x: -20, opacity: 0 }}
                      animate={{ x: 0, opacity: 1 }}
                      transition={{ delay: 0.1 + i * 0.05 }}
                      onClick={() => handleMobileNavClick(tab)}
                      className={`text-2xl font-black tracking-tight text-left flex items-center gap-3
                        ${currentPage === tab.page ? 'text-nofx-gold' : 'text-zinc-500'}`}
                    >
                      {currentPage === tab.page && (
                        <motion.div
                          layoutId="active-indicator"
                          className="w-1.5 h-1.5 rounded-full bg-nofx-gold"
                        />
                      )}
                      {tab.label}
                      {tab.requiresAuth && !isLoggedIn && (
                        <span className="text-[10px] px-1.5 py-0.5 rounded border border-zinc-800 text-zinc-500 font-normal tracking-wide uppercase align-middle relative -top-1">
                          LOGIN_REQ
                        </span>
                      )}
                    </motion.button>
                  ))
                })()}

                {/* Original Page Links */}
                {isHomePage && (
                  <div className="pt-6 border-t border-white/5 space-y-4">
                    {[
                      { key: 'features', label: t('features', language) },
                      { key: 'howItWorks', label: t('howItWorks', language) },
                    ].map((item, i) => (
                      <motion.a
                        key={item.key}
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        transition={{ delay: 0.5 + i * 0.1 }}
                        href={`#${item.key === 'features' ? 'features' : 'how-it-works'}`}
                        className="block text-lg font-mono text-zinc-600 hover:text-white"
                        onClick={() => setMobileMenuOpen(false)}
                      >
                        {'>'} {item.label}
                      </motion.a>
                    ))}
                  </div>
                )}
              </div>

              {/* Bottom Actions */}
              <div className="mt-auto space-y-8">
                {/* Auth Actions */}
                {isLoggedIn && user ? (
                  <button
                    onClick={() => {
                      onLogout?.()
                      setMobileMenuOpen(false)
                    }}
                    className="w-full bg-red-500/10 border border-red-500/20 text-red-500 rounded-lg font-bold text-sm hover:bg-red-500/20 transition-colors"
                  >
                    {t('exitLogin', language)}
                  </button>
                ) : (
                  currentPage !== 'login' && currentPage !== 'register' && (
                    <a
                      href="/login"
                      className="flex items-center justify-center bg-nofx-gold text-black rounded-lg font-bold text-sm hover:bg-yellow-400 transition-colors"
                    >
                      {t('signIn', language)}
                    </a>
                  )
                )}
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </nav>
  )
}
