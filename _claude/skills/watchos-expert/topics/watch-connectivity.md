# Watch-iPhone Communication (WatchConnectivity)

### Session Setup

```swift
import WatchConnectivity

class WatchSessionManager: NSObject, ObservableObject, WCSessionDelegate {
    static let shared = WatchSessionManager()

    @Published var receivedMessage: [String: Any] = [:]

    override init() {
        super.init()
        if WCSession.isSupported() {
            WCSession.default.delegate = self
            WCSession.default.activate()
        }
    }

    // MARK: - WCSessionDelegate

    func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: Error?) {
        if let error = error {
            print("WCSession activation failed: \(error)")
        }
    }

    #if os(iOS)
    func sessionDidBecomeInactive(_ session: WCSession) {}
    func sessionDidDeactivate(_ session: WCSession) {
        session.activate()
    }
    #endif
}
```

### Sending Data

```swift
// Send message (requires reachability)
func sendMessage(_ message: [String: Any]) {
    guard WCSession.default.isReachable else { return }

    WCSession.default.sendMessage(message, replyHandler: { reply in
        print("Reply: \(reply)")
    }, errorHandler: { error in
        print("Error: \(error)")
    })
}

// Transfer user info (queued, delivered when possible)
func transferUserInfo(_ userInfo: [String: Any]) {
    WCSession.default.transferUserInfo(userInfo)
}

// Update application context (latest state only)
func updateContext(_ context: [String: Any]) {
    try? WCSession.default.updateApplicationContext(context)
}
```

### Receiving Data

```swift
// Receive messages
func session(_ session: WCSession, didReceiveMessage message: [String : Any]) {
    DispatchQueue.main.async {
        self.receivedMessage = message
    }
}

// Receive user info
func session(_ session: WCSession, didReceiveUserInfo userInfo: [String : Any] = [:]) {
    DispatchQueue.main.async {
        // Process userInfo
    }
}

// Receive application context
func session(_ session: WCSession, didReceiveApplicationContext applicationContext: [String : Any]) {
    DispatchQueue.main.async {
        // Update app state
    }
}
```
