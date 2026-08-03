import AppKit
import Foundation
import UserNotifications

enum NotificationServiceError: Error, CustomStringConvertible {
    case encodingFailed
    case sendFailed(Error)

    var description: String {
        switch self {
        case .encodingFailed:
            return "Failed to encode click action to JSON"
        case .sendFailed(let error):
            return "Failed to send notification: \(error.localizedDescription)"
        }
    }
}

final class UNNotificationService: NotificationSending {

    private let center: UNUserNotificationCenter

    init(center: UNUserNotificationCenter = .current()) {
        self.center = center
    }

    func send(config: NotificationConfig, completion: @escaping (Result<Void, Error>) -> Void) {
        // Set app icon explicitly so macOS picks it up for notifications
        if let iconURL = Bundle.main.url(forResource: "AppIcon", withExtension: "icns"),
           let icon = NSImage(contentsOf: iconURL) {
            NSApplication.shared.applicationIconImage = icon
        }

        let content = UNMutableNotificationContent()
        content.title = config.title
        content.body = config.message
        content.sound = config.silent ? nil : .default
        content.categoryIdentifier = "AGENT_NOTIFICATION"

        if let subtitle = config.subtitle {
            content.subtitle = subtitle
        }

        if let threadID = config.threadID {
            content.threadIdentifier = threadID
        }

        if #available(macOS 12.0, *) {
            if config.timeSensitive {
                content.interruptionLevel = .timeSensitive
            }
        }

        if let actionJSON = config.action.toJSON() {
            content.userInfo["action"] = actionJSON
        }

        let identifier = config.group ?? UUID().uuidString

        let request = UNNotificationRequest(
            identifier: identifier,
            content: content,
            trigger: nil
        )

        center.add(request) { error in
            if let error = error {
                completion(.failure(NotificationServiceError.sendFailed(error)))
            } else {
                completion(.success(()))
            }
        }
    }
}
