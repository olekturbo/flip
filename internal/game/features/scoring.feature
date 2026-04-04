Feature: Round score calculation
  The round score is computed from a player's hand.
  Number cards are summed first; ×2 or ÷2 then scales that sum (they cancel if
  both held); +/- modifier cards are added afterwards. Minimum score is 0.

  Scenario: Plain number cards sum together
    Given a player holds [3, 5, 7]
    Then their round score is 15

  Scenario: The zero card contributes nothing
    Given a player holds [0, 8]
    Then their round score is 8

  Scenario: The ×2 modifier doubles the number-card total
    Given a player holds [4, 6, ×2]
    Then their round score is 20

  Scenario: The ÷2 modifier halves the number-card total, rounded down
    Given a player holds [5, 7, ÷2]
    Then their round score is 6

  Scenario: Holding both ×2 and ÷2 — they cancel each other out
    Given a player holds [6, ×2, ÷2]
    Then their round score is 6

  Scenario: Positive modifier is added after scaling
    Given a player holds [3, 4, ×2, +10]
    Then their round score is 24

  Scenario: Negative modifier is subtracted after scaling
    Given a player holds [5, 3, ×2, -4]
    Then their round score is 12

  Scenario: Multiple modifiers combine
    Given a player holds [4, +10, -2]
    Then their round score is 12

  Scenario: Score never goes below zero
    Given a player holds [1, -10]
    Then their round score is 0

  Scenario: Negative modifier on a zero-value hand floors to zero
    Given a player holds [0, -4]
    Then their round score is 0

  Scenario: Action cards in hand do not affect the score
    Given a player holds [7, 2nd Chance, Freeze]
    Then their round score is 7
